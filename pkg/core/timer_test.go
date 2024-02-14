package core

import (
	"fmt"
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

func TestWaitForBeginTime(t *testing.T) {
	tests := []struct {
		name  string
		begin string
		from  string
		wait  time.Duration
		err   error
	}{
		{"wait one minute", "+1", "2018-10-10T10:00:00Z", 1 * time.Minute, nil},
		{"wait 999999 minutes", "+999999", "2018-10-10T10:00:00Z", 999999 * time.Minute, nil},
		{"wait until 10:23", "1023", "2018-10-10T10:00:00Z", 23 * time.Minute, nil},
		{"wait until 23:59", "2359", "2018-10-10T10:00:00Z", 13*time.Hour + 59*time.Minute, nil},
		{"wait until 9:59", "0959", "2018-10-10T10:00:00Z", 23*time.Hour + 59*time.Minute, nil},
		{"fail text", "today", "2018-10-10T10:00:00Z", time.Duration(0), fmt.Errorf("invalid format for begin delay 'today'")},
		{"fail number", "1", "2018-10-10T10:00:00Z", time.Duration(0), fmt.Errorf("invalid format for begin delay '1'")},
		{"fail +hour", "+1h", "2018-10-10T10:00:00Z", time.Duration(0), fmt.Errorf("invalid format for begin delay '+1h'")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, err := time.Parse(time.RFC3339, tt.from)
			if err != nil {
				t.Fatalf("unable to parse from %s: %v", tt.from, err)
			}
			result, err := waitForBeginTime(tt.begin, from)
			switch {
			case (err != nil && tt.err == nil) || (err == nil && tt.err != nil) || (err != nil && tt.err != nil && err.Error() != tt.err.Error()):
				t.Errorf("waitForBeginTime(%s, %s) error = %v, wantErr %v", tt.begin, tt.from, err, tt.err)
			case result != tt.wait:
				t.Errorf("waitForBeginTime(%s, %s) = %v, want %v", tt.begin, tt.from, result, tt.wait)
			}
		})
	}
}
