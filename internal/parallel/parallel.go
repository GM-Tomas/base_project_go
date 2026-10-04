// Package parallel runs a request's independent reads at the same time.
package parallel

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

// Run calls every fn at once and waits for all of them. The context they get is canceled as soon as one
// fails, and that first error is the one returned. That much is errgroup.WithContext; the difference is a
// panic: errgroup lets it crash the process, Run returns it as an error carrying the stack, so it fails
// the request (logged with its traceId) as a panic in the request's own goroutine would.
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
			err = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
		}
	}()
	return fn(ctx)
}
