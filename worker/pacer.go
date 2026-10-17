package worker

import (
	"context"
	"fmt"
	"time"
	"net/http"
	"sync"
)

func Pace(
	ctx context.Context,
	endpoint string,
	client *http.Client,
	rps int,
	duration time.Duration,
	maxInFlight int,
) error {

	if rps <= 0 {
		return fmt.Errorf("RPS must be greater than zero")
	}

	if duration <= 0 {
		return fmt.Errorf("duration must be greater than zero")
	}

	if maxInFlight <= 0 {
		return fmt.Errorf("maxInFlight must be greater than zero")
	}

	if client == nil {
		return fmt.Errorf("http client must not be nil")
	}

	if client.Timeout <= 0 {
		return fmt.Errorf("http client timeout (%s) is not positive", client.Timeout)
	}

	interval := time.Second / time.Duration(rps)

	if interval <= 0 {
		return fmt.Errorf("RPS is too high, resulting in a non-positive interval")
	}

	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(interval)
	timer := time.NewTimer(duration)

	slots := make(chan struct{} , maxInFlight)

	var wg sync.WaitGroup

	defer ticker.Stop()
	defer timer.Stop()
	defer wg.Wait() // Wait for all goroutines to finish before returning


	for {
		select {
		case <-ticker.C:
			fmt.Println("Tick")
			if err := ctx.Err(); err != nil {
				fmt.Println("Context canceled, stopping")
				return err
			}

			if !time.Now().Before(deadline) {
				fmt.Println("Duration elapsed, stopping")
				return nil
			}

			select {
				case slots <- struct{}{}:
					wg.Add(1)
					go func() {
						defer wg.Done()
						defer func() { <-slots }() // Release the slot when done
						// Perform the HTTP request here
						_, err := SendOnce(ctx, client, endpoint)
						if err != nil {
							fmt.Printf("Error sending request: %v\n", err)
						}
					}()
				
				default:
					fmt.Println("Max in-flight requests reached, skipping this tick")
			}
		case <-timer.C:
			fmt.Println("Duration elapsed, stopping")
			return nil
		case <-ctx.Done():
			fmt.Println("Context canceled, stopping")
			return ctx.Err()
		}
	}

}