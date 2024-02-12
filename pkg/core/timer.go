package core

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/robfig/cron/v3"
)

type TimerOptions struct {
	Once      bool
	Cron      string
	Begin     string
	Frequency int
}

type Update struct {
	// Last whether or not this is the last update, and no more will be coming.
	// If true, perform this action and then end.
	Last bool
}

func sendTimer(c chan Update, last bool) {
	// make the channel write non-blocking
	select {
	case c <- Update{Last: last}:
	default:
	}
}

// Time start a timer that tells when to run an activity, based on its options.
// Each time to run an activity is indicated via a message in a channel.
func Timer(opts TimerOptions) (<-chan Update, error) {
	var (
		delay time.Duration
		err   error
	)

	now := time.Now()
	// parse the options to determine our delays
	if opts.Cron != "" {
		// calculate delay until next cron moment as defined
		delay, err = waitForCron(opts.Cron, now)
		if err != nil {
			return nil, fmt.Errorf("invalid cron format '%s': %v", opts.Cron, err)
		}
	} else if opts.Begin != "" {
		// calculate how long to wait
		minsRe, err := regexp.Compile(`^\+([0-9]+)$`)
		if err != nil {
			return nil, fmt.Errorf("invalid matcher for checking begin delay options: %v", err)
		}
		timeRe, err := regexp.Compile(`([0-9][0-9])([0-9][0-9])`)
		if err != nil {
			return nil, fmt.Errorf("invalid matcher for checking begin delay options: %v", err)
		}

		// first look for +MM, which means delay MM minutes
		delayMinsParts := minsRe.FindStringSubmatch(opts.Begin)
		startTimeParts := timeRe.FindStringSubmatch(opts.Begin)

		switch {
		case len(delayMinsParts) > 1:
			delayMins, err := strconv.Atoi(delayMinsParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid format for begin delay '%s': %v", opts.Begin, err)
			}
			delay = time.Duration(delayMins) * time.Minute
		case len(startTimeParts) > 3:
			hour, err := strconv.Atoi(startTimeParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid format for begin delay '%s': %v", opts.Begin, err)
			}
			minute, err := strconv.Atoi(startTimeParts[2])
			if err != nil {
				return nil, fmt.Errorf("invalid format for begin delay '%s': %v", opts.Begin, err)
			}

			// convert that start time into a Duration to wait
			now := time.Now()

			today := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, now.Second(), now.Nanosecond(), time.UTC)
			if today.After(now) {
				delay = today.Sub(now)
			} else {
				// add one day
				delay = today.Add(24 * time.Hour).Sub(now)
			}
		default:
			return nil, fmt.Errorf("invalid format for begin delay '%s': %v", opts.Begin, err)
		}
	}

	// if delayMins is 0, this will do nothing, so it does not hurt
	time.Sleep(delay)

	c := make(chan Update)
	go func(opts TimerOptions) {
		// when this goroutine ends, close the channel
		defer close(c)

		// if once, ignore all delays and go
		if opts.Once {
			sendTimer(c, true)
			return
		}

		// create our delay and timer loop and go
		for {
			lastRun := time.Now()

			// not once - run the first backup
			sendTimer(c, false)

			if opts.Cron != "" {
				delay, _ = waitForCron(opts.Cron, now)
			} else {
				// calculate how long until the next run
				// just take our last start time, and add the frequency until it is past our
				// current time. We cannot just take the last time and add,
				// because it might have been during a backup run
				now := time.Now()
				diff := int(now.Sub(lastRun).Minutes())
				// make sure we at least wait one full frequency
				if diff == 0 {
					diff += opts.Frequency
				}
				passed := diff % opts.Frequency
				delay = time.Duration(opts.Frequency-passed) * time.Minute
			}

			// if delayMins is 0, this will do nothing, so it does not hurt
			time.Sleep(delay)
		}
	}(opts)
	return c, nil
}

// waitForCron given the current time and a cron string, calculate the Duration
// until the next time we will match the cron
func waitForCron(cronExpr string, from time.Time) (time.Duration, error) {
	sched, err := cron.ParseStandard(cronExpr)
	if err != nil {
		return time.Duration(0), err
	}
	// sched.Next() returns the next time that the cron expression will match, beginning in 1ns;
	// we allow matching current time, so we do it from 1ns
	next := sched.Next(from.Add(-1 * time.Nanosecond))
	return next.Sub(from), nil
}
