// Package schedule builds staircase traffic schedules without sending requests.
package schedule

import (
	"errors"
	"time"
)

// TestSpec contains the settings for one staircase test.
type TestSpec struct {
	MinRPS       int
	PeakRPS      int
	StepRPS      int
	HoldDuration time.Duration
}

// Stage describes one traffic level and how long to hold it.
type Stage struct {
	RPS      int
	Duration time.Duration
}

// BuildSchedule converts a test specification into traffic stages.
func BuildSchedule(spec TestSpec) ([]Stage, error) {
	if spec.MinRPS <= 0 {
		return nil, errors.New("MinRPS must be greater than zero")
	}
	if spec.PeakRPS <= 0 {
		return nil, errors.New("PeakRPS must be greater than zero")
	}
	if spec.StepRPS <= 0 {
		return nil, errors.New("StepRPS must be greater than zero")
	}
	if spec.HoldDuration <= 0 {
		return nil, errors.New("HoldDuration must be greater than zero")
	}
	if spec.MinRPS > spec.PeakRPS {
		return nil, errors.New("minimum RPS must not exceed peak RPS")
	}

	var stages []Stage
	for rate := spec.MinRPS; ; {
		stages = append(stages, Stage{RPS: rate, Duration: spec.HoldDuration})
		if rate == spec.PeakRPS {
			return stages, nil
		}
		// Compare the remaining distance before adding to avoid integer overflow.
		if spec.StepRPS >= spec.PeakRPS-rate {
			rate = spec.PeakRPS
		} else {
			rate += spec.StepRPS
		}
	}
}
