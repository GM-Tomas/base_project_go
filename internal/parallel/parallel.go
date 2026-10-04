// Package parallel runs a request's independent reads at the same time.
package parallel

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
)

// Run calls every fn at once and waits for all of them. The context they get is canceled as soon as one
// fails, and that first error is the one returned. A panic in one is recovered (and logged with its
// stack) and returned as an error: it fails the request, as a panic in the request's own goroutine does,
// rather than the whole process.
func Run(ctx context.Context, fns ...func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := call(ctx, fn); err != nil {
				once.Do(func() {
					first = err
					cancel()
				})
			}
		}()
	}
	wg.Wait()
	return first
}

func call(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("panic: %v\n%s", p, debug.Stack())
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn(ctx)
}
