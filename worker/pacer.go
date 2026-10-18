package worker

import (
	"context"
	"fmt"
	"time"
	"net/http"
	"sync"
)

type Summary struct {
	Started int
	Completed int
	Succeeded int
	Failed int
	Skipped int
	TotalDuration time.Duration
}

func (s Summary) AverageDuration() time.Duration {
	if s.Completed == 0 {
		return 0
	}

	return s.TotalDuration / time.Duration(s.Completed)
}

func Pace(
	ctx context.Context,
	endpoint string,
	client *http.Client,
	rps int,
	duration time.Duration,
	maxInFlight int,
) (Summary, error) {

	if rps <= 0 {
		return Summary{}, fmt.Errorf("RPS must be greater than zero")
	}

	if duration <= 0 {
		return Summary{}, fmt.Errorf("duration must be greater than zero")
	}

	if maxInFlight <= 0 {
		return Summary{},fmt.Errorf("maxInFlight must be greater than zero")
	}

	if client == nil {
		return Summary{}, fmt.Errorf("http client must not be nil")
	}

	if client.Timeout <= 0 {
		return Summary{}, fmt.Errorf("http client timeout (%s) is not positive", client.Timeout)
	}

	interval := time.Second / time.Duration(rps)

	if interval <= 0 {
		return Summary{}, fmt.Errorf("RPS is too high, resulting in a non-positive interval")
	}

	summary := Summary{}

	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(interval)
	timer := time.NewTimer(duration)

	slots := make(chan struct{} , maxInFlight)

	var wg sync.WaitGroup
	var mu sync.Mutex

	defer ticker.Stop()
	defer timer.Stop()


	for {
		select {
		case <-ticker.C:
			fmt.Println("Tick")
			if err := ctx.Err(); err != nil {
				fmt.Println("Context canceled, stopping")
				wg.Wait()
				return summary, err
			}

			if !time.Now().Before(deadline) {
				fmt.Println("Duration elapsed, stopping")
				wg.Wait()
				return summary, nil
			}

			select {
				case slots <- struct{}{}:

					mu.Lock()
					summary.Started++
					mu.Unlock()

					wg.Add(1)

					go func() {
						defer wg.Done()
						defer func() { <-slots }() // Release the slot when done
						defer func() {
							mu.Lock()
							summary.Completed++
							mu.Unlock()
						}() // Increment completed count when done
						// Perform the HTTP request here
						resp, err := SendOnce(ctx, client, endpoint)

						if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
							mu.Lock()
							summary.Succeeded++
							mu.Unlock()
						} else {
							mu.Lock()
							summary.Failed++
							mu.Unlock()
							if err != nil {
								fmt.Printf("Request failed: %v\n", err)
							} else {
								fmt.Printf("Request returned unsuccessful HTTP status: %d\n", resp.StatusCode)
							}
						}

						mu.Lock()
						summary.TotalDuration += resp.Duration
						mu.Unlock()
						
					}()
				
				default:
					mu.Lock()
					summary.Skipped++
					mu.Unlock()
					fmt.Println("Max in-flight requests reached, skipping this tick")
			}
		case <-timer.C:
			fmt.Println("Duration elapsed, stopping")
			wg.Wait()
			return summary, nil
		case <-ctx.Done():
			fmt.Println("Context canceled, stopping")
			wg.Wait()
			return summary, ctx.Err()
		}
	}

}