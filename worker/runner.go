package worker

import (
	"load_tester/schedule"
	"context"
	"net/http"
	"fmt"
)

type StageResult struct {
    Stage   schedule.Stage
    Summary Summary
}

func RunSchedule(
    ctx context.Context,
    client *http.Client,
    endpoint string,
    stages []schedule.Stage,
    maxInFlight int,
) ([]StageResult, error) {

	if stages == nil || len(stages) == 0 {
		return nil, fmt.Errorf("stages must not be nil or empty")
	}

	for i, stage := range stages {
		if stage.RPS <= 0 {
			return nil, fmt.Errorf("stage %d has non-positive RPS: %d", i, stage.RPS)
		}
		if stage.Duration <= 0 {
			return nil, fmt.Errorf("stage %d has non-positive duration: %s", i, stage.Duration)
		}
	}

	var results []StageResult

	for i, stage := range stages {

		summary, err := Pace(
			ctx,
			endpoint,
			client,
			stage.RPS,
			stage.Duration,
			maxInFlight,
		)

		results = append(results, StageResult{
			Stage:   stage,
			Summary: summary,
		})

		if err != nil {
			return results, fmt.Errorf("error during stage %d: %w", i, err)
		}

	}

	return results, nil

}