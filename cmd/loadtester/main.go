package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"load_tester/schedule"
)

func main() {
	minRPS := flag.Int("min-rps", 100, "Starting requests per second")
	peakRPS := flag.Int("peak-rps", 500, "Maximum requests per second")
	stepRPS := flag.Int("step-rps", 100, "Requests per second added at each step")
	holdDuration := flag.Duration("hold-duration", 30*time.Second, "Time spent at each level, e.g. 30s or 1m")
	flag.Parse()

	spec := schedule.TestSpec{
		MinRPS:       *minRPS,
		PeakRPS:      *peakRPS,
		StepRPS:      *stepRPS,
		HoldDuration: *holdDuration,
	}

	stages, err := schedule.BuildSchedule(spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	var total time.Duration
	for i, stage := range stages {
		fmt.Printf("Stage %d: %d RPS for %s\n", i+1, stage.RPS, stage.Duration)
		total += stage.Duration
	}
	fmt.Printf("Total duration: %s\n", total)
}
