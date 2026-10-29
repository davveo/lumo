package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkerCleanupCancelsAndWaitsBeforeDependenciesCanClose(t *testing.T) {
	entered, cancelled := make(chan struct{}, 2), make(chan struct{}, 2)
	release := make(chan struct{})
	worker := func(ctx context.Context) error {
		entered <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		<-release
		return nil
	}
	_, finish := startWorkers(context.Background(), worker, worker)
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
	}
	done := make(chan struct{})
	go func() { finish(); close(done) }()
	for range 2 {
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("cleanup did not cancel both workers")
		}
	}
	select {
	case <-done:
		t.Fatal("cleanup returned while an in-flight worker still needs dependencies")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish after bounded workers exited")
	}
	// Dependency cleanup follows this return; repeated cleanup remains safe.
	finish()
}

func TestWorkerFailureStillAllowsSiblingCancellationAndCompleteDrain(t *testing.T) {
	want := errors.New("RPC listener failed")
	sibling := make(chan struct{})
	failures, finish := startWorkers(context.Background(),
		func(context.Context) error { return want },
		func(ctx context.Context) error { <-ctx.Done(); close(sibling); return nil },
	)
	select {
	case got := <-failures:
		if !errors.Is(got, want) {
			t.Fatalf("original worker failure was hidden: %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("worker failure was not delivered")
	}
	done := make(chan struct{})
	go func() { finish(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup waited for an unread failure channel")
	}
	select {
	case <-sibling:
	default:
		t.Fatal("a sibling outlived shared resource cleanup")
	}
}
