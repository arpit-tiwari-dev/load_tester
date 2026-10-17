package worker

import (
	"context"
	"fmt"
	"net/http"
	"io"
	"time"
)

type Result struct {
    StatusCode int
    Duration   time.Duration
}

func SendOnce(ctx context.Context, client *http.Client, endpoint string) (Result, error){

	req, err := http.NewRequestWithContext(
		ctx,        // control / cancellation
		"GET",      // HTTP method
		endpoint,        // destination
		nil,        // request body
	)

	if err != nil {
		return Result{}, fmt.Errorf("failed to create request: %w", err)
	}

	start := time.Now()

	resp, err := client.Do(req)
	if err != nil {
		return Result{Duration: time.Since(start)}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return Result{StatusCode: resp.StatusCode, Duration: time.Since(start)}, fmt.Errorf("failed to read response body: %w", err)
	}

	duration := time.Since(start)

	return Result{StatusCode: resp.StatusCode, Duration: duration}, nil

}