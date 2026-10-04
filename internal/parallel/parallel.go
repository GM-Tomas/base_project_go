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
// fails, and that first error is the one returned. That much is errgroup.WithContext; the difference is a
// panic: errgroup lets it crash the process, Run returns it as an error carrying the stack (on one line, so
// the request's traceId log line holds all of it), so it fails the request with a 500 as a panic in the
// request's own goroutine would. It never wraps the panic's value: a domain error in it mustn't turn into
// a 4xx whose detail is the stack. A panic that loses the race to an earlier error is logged here instead.
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
			panicked, err := call(ctx, fn)
			if err == nil {
				return
			}
			kept := false
			once.Do(func() {
				first, kept = err, true
				cancel()
			})
			if panicked && !kept {
				log.Print(err)
			}
		}()
	}
	wg.Wait()
	return first
}

func call(ctx context.Context, fn func(context.Context) error) (panicked bool, err error) {
	defer func() {
		if p := recover(); p != nil {
			panicked, err = true, fmt.Errorf("panic: %v (stack: %q)", p, debug.Stack())
		}
	}()
	return false, fn(ctx)
}
