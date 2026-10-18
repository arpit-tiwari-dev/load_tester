package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"load_tester/schedule"
)

func TestRunScheduleValidatesBeforeTraffic(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	valid := schedule.Stage{RPS: 100, Duration: 50 * time.Millisecond}
	for _, tt := range []struct {
		name   string
		stages []schedule.Stage
	}{
		{"nil", nil},
		{"empty", []schedule.Stage{}},
		{"later invalid rate", []schedule.Stage{valid, {RPS: 0, Duration: time.Second}}},
		{"later invalid duration", []schedule.Stage{valid, {RPS: 100, Duration: -time.Second}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			results, err := RunSchedule(context.Background(), client, server.URL, tt.stages, 2)
			if err == nil || len(results) != 0 {
				t.Fatalf("want no results and validation error, got (%v, %v)", results, err)
			}
			if requests.Load() != 0 {
				t.Fatal("invalid schedule generated traffic")
			}
		})
	}
}

func TestRunScheduleStageResults(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			client := server.Client()
			client.Timeout = time.Second
			stages := []schedule.Stage{{RPS: 100, Duration: 100 * time.Millisecond}, {RPS: 200, Duration: 100 * time.Millisecond}}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			results, err := RunSchedule(ctx, client, server.URL, stages, 2)
			if err != nil || len(results) != len(stages) {
				t.Fatalf("got (%v, %v), want two stage results", results, err)
			}
			for i, result := range results {
				if result.Stage != stages[i] {
					t.Errorf("stage %d out of order: %+v", i, result.Stage)
				}
				s := result.Summary
				if s.Started == 0 || s.Started != s.Completed || s.Completed != s.Succeeded+s.Failed {
					t.Fatalf("invalid summary: %+v", s)
				}
				if status == http.StatusOK && s.Succeeded != s.Completed {
					t.Errorf("expected all successes: %+v", s)
				}
				if status == http.StatusInternalServerError && s.Failed != s.Completed {
					t.Errorf("expected all failures: %+v", s)
				}
			}
		})
	}
}

func TestRunSchedulePreservesCanceledStage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	stages := []schedule.Stage{{RPS: 100, Duration: time.Second}, {RPS: 200, Duration: time.Second}}
	results, err := RunSchedule(ctx, client, server.URL, stages, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want wrapped cancellation, got %v", err)
	}
	if len(results) != 1 || results[0].Stage != stages[0] {
		t.Fatalf("want only interrupted stage, got %+v", results)
	}
	s := results[0].Summary
	if s.Started != 1 || s.Completed != 1 || s.Failed != 1 {
		t.Fatalf("lost partial results: %+v", s)
	}
}
