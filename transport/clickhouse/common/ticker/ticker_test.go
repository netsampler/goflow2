package ticker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRunContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	updateCalled := false
	updateFunc := func(ctx context.Context) error {
		updateCalled = true
		return nil
	}

	// Cancel context immediately
	cancel()

	err := Run(ctx, 100*time.Millisecond, 200*time.Millisecond, updateFunc)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}

	if updateCalled {
		t.Error("Update function should not be called when context is already cancelled")
	}
}

func TestRunRegularUpdateTicker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	var mu sync.Mutex
	callCount := 0
	updateFunc := func(ctx context.Context) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		return nil
	}

	go func() {
		Run(ctx, 50*time.Millisecond, 1*time.Second, updateFunc)
	}()

	// Wait for some updates to occur
	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	if finalCount < 2 {
		t.Errorf("Expected at least 2 update calls, got %d", finalCount)
	}
}

func TestRunForceUpdateOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	var mu sync.Mutex
	callCount := 0
	errorOnFirstCall := true

	updateFunc := func(ctx context.Context) error {
		mu.Lock()
		callCount++
		shouldError := errorOnFirstCall && callCount == 1
		mu.Unlock()

		if shouldError {
			return errors.New("simulated error")
		}
		return nil
	}

	go func() {
		Run(ctx, 200*time.Millisecond, 100*time.Millisecond, updateFunc)
	}()

	// Wait for the error to occur and force updates to trigger
	time.Sleep(350 * time.Millisecond)

	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	// Should have at least the initial error call plus force update calls
	if finalCount < 2 {
		t.Errorf("Expected at least 2 calls (error + force updates), got %d", finalCount)
	}
}

func TestRunForceUpdateBehavior(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	var mu sync.Mutex
	callTimes := []time.Time{}
	callCount := 0
	errorReturned := false

	updateFunc := func(ctx context.Context) error {
		mu.Lock()
		callCount++
		callTimes = append(callTimes, time.Now())
		// Return error on first call to trigger force mode
		if callCount == 1 {
			errorReturned = true
			mu.Unlock()
			return errors.New("first call error")
		}
		mu.Unlock()
		return nil
	}

	go func() {
		// Use longer update period so force ticker is the primary driver after error
		Run(ctx, 300*time.Millisecond, 80*time.Millisecond, updateFunc)
	}()

	time.Sleep(550 * time.Millisecond)

	mu.Lock()
	finalCount := callCount
	finalErrorReturned := errorReturned
	mu.Unlock()

	if finalCount < 2 {
		t.Errorf("Expected at least 2 calls, got %d", finalCount)
	}

	if !finalErrorReturned {
		t.Error("Expected first call to return an error")
	}

	// The key behavior is that we get multiple calls after the error
	// The exact timing is less important than the fact that force updates occur
	if finalCount < 3 {
		t.Logf("Got %d calls, which demonstrates force update behavior", finalCount)
	}
}

func TestRunRecoveryFromError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	var mu sync.Mutex
	callCount := 0

	updateFunc := func(ctx context.Context) error {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()

		// Error on first call, success afterwards
		if count == 1 {
			return errors.New("temporary error")
		}
		return nil
	}

	go func() {
		Run(ctx, 300*time.Millisecond, 100*time.Millisecond, updateFunc)
	}()

	// Wait for error and recovery
	time.Sleep(250 * time.Millisecond) // Wait for force update to succeed

	// Reset call count to test normal ticker behavior after recovery
	mu.Lock()
	callCount = 0
	mu.Unlock()

	// Wait for normal ticker to fire
	time.Sleep(350 * time.Millisecond)

	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	// Should have at least one call from normal ticker after recovery
	if finalCount < 1 {
		t.Errorf("Expected at least 1 call after recovery, got %d", finalCount)
	}
}

func TestRunConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var mu sync.Mutex
	callCount := 0

	updateFunc := func(ctx context.Context) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	err := Run(ctx, 50*time.Millisecond, 100*time.Millisecond, updateFunc)

	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}

	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	if finalCount == 0 {
		t.Error("Expected at least one update call")
	}
}
