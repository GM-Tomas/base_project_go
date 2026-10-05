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
// panic: errgroup lets it crash the process, Run returns it as an error carrying the stack (on one line, so
// the request's traceId log line holds all of it), so it fails the request with a 500 as a panic in the
// request's own goroutine would. A panic is a bug, so it fails the run even after another error, naming that
// one too. Neither is wrapped: a domain error in either mustn't turn into a 4xx whose detail is the stack.
func Run(ctx context.Context, fns ...func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg            sync.WaitGroup
		mu            sync.Mutex
		first         error
		firstPanicked bool
		laterPanic    error
	)
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			panicked, err := call(ctx, fn)
			if err == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case first == nil:
				first, firstPanicked = err, panicked
				cancel()
			case panicked && !firstPanicked && laterPanic == nil:
				laterPanic = err
			}
		}()
	}
	wg.Wait()
	if laterPanic != nil {
		return fmt.Errorf("%v, after another call failed: %v", laterPanic, first)
	}
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
