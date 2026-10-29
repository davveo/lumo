package main

import (
	"context"
	"sync"
)

// The returned cleanup must run before shared DB/RPC dependencies are closed.
// RPC owns its bounded graceful-stop period; event work inherits cancellation.
func startWorkers(parent context.Context, work ...func(context.Context) error) (<-chan error, func()) {
	ctx, cancel := context.WithCancel(parent)
	failures := make(chan error, len(work))
	var workers sync.WaitGroup
	workers.Add(len(work))
	for _, run := range work {
		go func() {
			defer workers.Done()
			failures <- run(ctx)
		}()
	}
	return failures, func() {
		cancel()
		workers.Wait()
	}
}
