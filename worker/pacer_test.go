package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPaceInvalidInputs(t *testing.T) {
	for _, tt := range []struct {
		name     string
		rps      int
		duration time.Duration
	}{
		{"zero rate", 0, time.Second},
		{"negative rate", -1, time.Second},
		{"zero duration", 10, 0},
		{"negative duration", 10, -time.Second},
		{"zero interval", 1_000_000_001, time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := Pace(ctx, "http://localhost", &http.Client{Timeout: time.Second}, tt.rps, tt.duration, 1); err == nil || errors.Is(err, context.Canceled) {
				t.Fatalf("want validation error before cancellation, got %v", err)
			}
		})
	}
}

func TestPaceLimitsConcurrencyAndDrains(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var mu sync.Mutex
	started := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		started++
		mu.Unlock()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer unblock()
	client := server.Client()
	client.Timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Pace(ctx, server.URL, client, 100, 200*time.Millisecond, 2) }()

	// Keep responses blocked beyond the pacing duration to exercise draining.
	time.Sleep(350 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Pace returned before outstanding requests finished: %v", err)
	default:
	}
	mu.Lock()
	count := started
	mu.Unlock()
	if count != 2 {
		t.Errorf("started %d blocked requests, want exactly 2", count)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Pace did not finish after responses were released")
	}
	mu.Lock()
	defer mu.Unlock()
	if started != 2 {
		t.Errorf("started %d requests, want no new requests after the deadline", started)
	}
}

func TestPaceCancelsInFlightRequest(t *testing.T) {
	entered := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
		stopped <- struct{}{}
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Pace(ctx, server.URL, client, 100, 5*time.Second, 1) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request never reached the server")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pace did not return after cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe request cancellation")
	}
}

func TestPaceCompletes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	const duration = 30 * time.Millisecond
	start := time.Now()
	if err := Pace(ctx, server.URL, client, 100, duration, 2); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < duration {
		t.Fatalf("returned after %s, before duration %s", elapsed, duration)
	}
}

func TestPaceCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Pace(ctx, "http://localhost", &http.Client{Timeout: time.Second}, 1, time.Second, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context canceled, got %v", err)
	}
}

func TestPaceDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := Pace(ctx, "http://localhost", &http.Client{Timeout: time.Second}, 1, time.Second, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

func TestPaceClientValidation(t *testing.T) {
	for _, tt := range []struct {
		name        string
		client      *http.Client
		maxInFlight int
	}{
		{"nil client", nil, 1},
		{"zero timeout", &http.Client{}, 1},
		{"negative timeout", &http.Client{Timeout: -time.Second}, 1},
		{"zero slots", &http.Client{Timeout: time.Second}, 0},
		{"negative slots", &http.Client{Timeout: time.Second}, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := Pace(ctx, "http://localhost", tt.client, 1, time.Second, tt.maxInFlight)
			if err == nil || errors.Is(err, context.Canceled) {
				t.Fatalf("want validation error, got %v", err)
			}
		})
	}
}
