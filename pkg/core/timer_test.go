package core

import (
	"testing"
	"time"
)

func TestWaitForCron(t *testing.T) {
	tests := []struct {
		name string
		cron string
		from string
		wait time.Duration
		err  error
	}{
		{"current minute", "1 * * * *", "2018-10-10T10:01:00Z", 0, nil},
		{"next minute", "1 * * * *", "2018-10-10T10:00:00Z", 1 * time.Minute, nil},
		{"next day by hour", "* 1 * * *", "2018-10-10T10:00:00Z", 15 * time.Hour, nil},
		{"current minute but seconds in", "1 * * * *", "2018-10-10T10:01:10Z", 59*time.Minute + 50*time.Second, nil}, // this line tests that we use the current minute, and not wait for "-10"
		{"midnight next day", "0 0 * * *", "2021-11-30T10:00:00Z", 14 * time.Hour, nil},
		{"first day next month in next year", "0 0 1 * *", "2020-12-30T10:00:00Z", 14*time.Hour + 24*time.Hour, nil}, // this line tests that we can handle rolling month correctly
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, err := time.Parse(time.RFC3339, tt.from)
			if err != nil {
				t.Fatalf("unable to parse from %s: %v", tt.from, err)
			}
			result, err := waitForCron(tt.cron, from)
			switch {
			case (err != nil && tt.err == nil) || (err == nil && tt.err != nil) || (err != nil && tt.err != nil && err.Error() != tt.err.Error()):
				t.Errorf("waitForCron(%s, %s) error = %v, wantErr %v", tt.cron, tt.from, err, tt.err)
			case result != tt.wait:
				t.Errorf("waitForCron(%s, %s) = %v, want %v", tt.cron, tt.from, result, tt.wait)
			}
		})
	}
}
