package worker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPaceSummaryClassification(t *testing.T) {
	for _, tt := range []struct {
		name        string
		status      int
		incomplete  bool
		wantSuccess bool
	}{
		{"success", 200, false, true},
		{"http failure", 500, false, false},
		{"incomplete success response", 200, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.incomplete {
					w.Header().Set("Content-Length", "100")
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, "hello")
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = time.Second
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			summary, err := Pace(ctx, server.URL, client, 100, 100*time.Millisecond, 2)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Started == 0 || summary.Started != summary.Completed || summary.Completed != summary.Succeeded+summary.Failed {
				t.Fatalf("inconsistent counters: %+v", summary)
			}
			if tt.wantSuccess && summary.Succeeded != summary.Completed {
				t.Errorf("all requests should succeed: %+v", summary)
			}
			if !tt.wantSuccess && summary.Failed != summary.Completed {
				t.Errorf("all requests should fail: %+v", summary)
			}
			if summary.TotalDuration <= 0 {
				t.Errorf("expected duration accumulation for completed requests: %+v", summary)
			}
		})
	}
}

func TestSummaryAverageDuration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		summary Summary
		want    time.Duration
	}{
		{"empty", Summary{}, 0},
		{"one request", Summary{Completed: 1, TotalDuration: 25 * time.Millisecond}, 25 * time.Millisecond},
		{"mixed outcomes", Summary{Completed: 4, Succeeded: 3, Failed: 1, TotalDuration: 100 * time.Millisecond}, 25 * time.Millisecond},
		{"fractional nanoseconds truncated", Summary{Completed: 2, TotalDuration: 5 * time.Nanosecond}, 2 * time.Nanosecond},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.summary.AverageDuration(); got != tt.want {
				t.Errorf("AverageDuration = %s, want %s", got, tt.want)
			}
		})
	}
}

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
			if _, err := Pace(ctx, "http://localhost", &http.Client{Timeout: time.Second}, tt.rps, tt.duration, 1); err == nil || errors.Is(err, context.Canceled) {
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
	go func() { _, err := Pace(ctx, server.URL, client, 100, 200*time.Millisecond, 2); done <- err }()

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
	go func() { _, err := Pace(ctx, server.URL, client, 100, 5*time.Second, 1); done <- err }()
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
	if _, err := Pace(ctx, server.URL, client, 100, duration, 2); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < duration {
		t.Fatalf("returned after %s, before duration %s", elapsed, duration)
	}
}

func TestPaceCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Pace(ctx, "http://localhost", &http.Client{Timeout: time.Second}, 1, time.Second, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context canceled, got %v", err)
	}
}

func TestPaceDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := Pace(ctx, "http://localhost", &http.Client{Timeout: time.Second}, 1, time.Second, 1); !errors.Is(err, context.DeadlineExceeded) {
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
			_, err := Pace(ctx, "http://localhost", tt.client, 1, time.Second, tt.maxInFlight)
			if err == nil || errors.Is(err, context.Canceled) {
				t.Fatalf("want validation error, got %v", err)
			}
		})
	}
}
