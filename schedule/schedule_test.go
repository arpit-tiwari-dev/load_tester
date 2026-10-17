package schedule

import (
	"testing"
	"time"
)

func TestBuildSchedule(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name            string
		min, peak, step int
		want            []int
	}{
		{"regular steps", 100, 500, 100, []int{100, 200, 300, 400, 500}},
		{"capped final step", 100, 450, 100, []int{100, 200, 300, 400, 450}},
		{"equal rates", 100, 100, 50, []int{100}},
		{"large step", 100, 150, 200, []int{100, 150}},
		{"integer boundary", maxInt - 1, maxInt, 10, []int{maxInt - 1, maxInt}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stages, err := BuildSchedule(TestSpec{tt.min, tt.peak, tt.step, 30 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if len(stages) != len(tt.want) {
				t.Fatalf("got %d stages, want %d", len(stages), len(tt.want))
			}
			var total time.Duration
			for i, stage := range stages {
				if stage.RPS != tt.want[i] || stage.Duration != 30*time.Second {
					t.Errorf("stage %d: got %+v, want %d RPS for 30s", i, stage, tt.want[i])
				}
				total += stage.Duration
			}
			if want := time.Duration(len(tt.want)) * 30 * time.Second; total != want {
				t.Errorf("total duration = %s, want %s", total, want)
			}
		})
	}
}

func TestBuildScheduleInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		spec TestSpec
	}{
		{"zero minimum", TestSpec{0, 500, 100, time.Second}},
		{"negative minimum", TestSpec{-1, 500, 100, time.Second}},
		{"zero peak", TestSpec{100, 0, 100, time.Second}},
		{"negative peak", TestSpec{100, -1, 100, time.Second}},
		{"zero step", TestSpec{100, 500, 0, time.Second}},
		{"negative step", TestSpec{100, 500, -1, time.Second}},
		{"zero duration", TestSpec{100, 500, 100, 0}},
		{"negative duration", TestSpec{100, 500, 100, -time.Second}},
		{"minimum above peak", TestSpec{500, 100, 100, time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stages, err := BuildSchedule(tt.spec)
			if err == nil || stages != nil {
				t.Fatalf("got (%v, %v), want nil stages and an error", stages, err)
			}
		})
	}
}
