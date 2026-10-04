package parallel_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/parallel"
	"github.com/stretchr/testify/assert"
)

func TestRun_CallsEveryFnAtOnce(t *testing.T) {
	// Each fn waits until all three have started: run one after another, they'd never finish.
	var started sync.WaitGroup
	started.Add(3)
	fn := func(ctx context.Context) error {
		started.Done()
		started.Wait()
		return nil
	}

	done := make(chan error)
	go func() { done <- parallel.Run(context.Background(), fn, fn, fn) }()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the fns didn't run at the same time")
	}
}

func TestRun_TheFirstErrorCancelsTheOthersAndIsReturned(t *testing.T) {
	broken := errors.New("snapshots unreadable")
	waiting := func(ctx context.Context) error {
		<-ctx.Done() // canceled by the failure below, or the test would hang
		return ctx.Err()
	}
	failing := func(context.Context) error { return broken }

	err := parallel.Run(context.Background(), waiting, failing, waiting)

	assert.ErrorIs(t, err, broken)
	assert.NotErrorIs(t, err, context.Canceled)
}

func TestRun_APanicIsAnError(t *testing.T) {
	err := parallel.Run(context.Background(),
		func(context.Context) error { return nil },
		func(context.Context) error { panic("bad data") },
	)

	assert.EqualError(t, err, "panic: bad data")
}

func TestRun_ACanceledCallerCancelsEveryFn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := parallel.Run(ctx, func(ctx context.Context) error { return ctx.Err() })

	assert.ErrorIs(t, err, context.Canceled)
	assert.NoError(t, parallel.Run(context.Background()), "nothing to run")
}
