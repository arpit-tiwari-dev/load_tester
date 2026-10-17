package worker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSendOnceResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "hello")
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = time.Second

			got, err := SendOnce(context.Background(), client, server.URL)
			if err != nil || got.StatusCode != status {
				t.Fatalf("SendOnce = (%+v, %v), want status %d and nil error", got, err, status)
			}
		})
	}
}

func TestSendOnceIncompleteBody(t *testing.T) {
	const delay = 25 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Promise more bytes than we send to simulate an interrupted response.
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(delay)
		_, _ = io.WriteString(w, "short")
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second

	status, err := SendOnce(context.Background(), client, server.URL)
	if status.StatusCode != http.StatusOK || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("SendOnce = (%+v, %v), want status 200 and unexpected EOF", status, err)
	}
	if status.Duration < delay {
		t.Errorf("duration = %s, want at least %s including the failed body read", status.Duration, delay)
	}
}

func TestSendOnceCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a canceled request should not reach the server")
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status, err := SendOnce(ctx, client, server.URL)
	if status.StatusCode != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("SendOnce = (%+v, %v), want status 0 and context canceled", status, err)
	}
}

func TestSendOnceInvalidURL(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	status, err := SendOnce(context.Background(), client, "://invalid")
	if status != (Result{}) || err == nil {
		t.Fatalf("SendOnce = (%+v, %v), want empty result and an error", status, err)
	}
}

func TestSendOnceDurationIncludesBody(t *testing.T) {
	const delay = 25 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Send headers now so the delay belongs to reading the body.
		w.(http.Flusher).Flush()
		time.Sleep(delay)
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second

	result, err := SendOnce(context.Background(), client, server.URL)
	if err != nil || result.StatusCode != http.StatusOK {
		t.Fatalf("SendOnce = (%+v, %v), want status 200 and nil error", result, err)
	}
	if result.Duration < delay {
		t.Errorf("duration = %s, want at least %s", result.Duration, delay)
	}
}

func TestSendOnceDurationOnTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Withhold headers until the client's timeout cancels the request.
		<-r.Context().Done()
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 50 * time.Millisecond

	result, err := SendOnce(context.Background(), client, server.URL)
	if result.StatusCode != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendOnce = (%+v, %v), want status 0 and deadline exceeded", result, err)
	}
	if result.Duration < client.Timeout {
		t.Errorf("duration = %s, want at least the timeout %s", result.Duration, client.Timeout)
	}
}
