package core

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// filenameRE is a regular expression to match a backup filename
var filenameRE = regexp.MustCompile(`^db_backup_(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})Z\.\w+$`)

// Prune prune older backups
func (e *Executor) Prune(opts PruneOptions) error {
	logger := e.Logger.WithField("run", opts.Run.String())
	logger.Info("beginning prune")
	var (
		candidates []string
		now        = opts.Now
	)
	if now.IsZero() {
		now = time.Now()
	}
	retainHours, err1 := convertToHours(opts.Retention)
	retainCount, err2 := convertToCount(opts.Retention)
	if err1 != nil && err2 != nil {
		return fmt.Errorf("invalid retention string: %s", opts.Retention)
	}
	if len(opts.Targets) == 0 {
		return errors.New("no targets")
	}

	for _, target := range opts.Targets {
		var pruned int

		logger.Debugf("pruning target %s", target)
		files, err := target.ReadDir(".", logger)
		if err != nil {
			return fmt.Errorf("failed to read directory: %v", err)
		}

		// create a slice with the filenames and their calculated times - these are *not* the timestamp times, but the times calculated from the filenames
		var filesWithTimes []fileWithTime

		for _, fileInfo := range files {
			filename := fileInfo.Name()
			matches := filenameRE.FindStringSubmatch(filename)
			if matches == nil {
				logger.Debugf("ignoring filename that is not standard backup pattern: %s", filename)
				continue
			}
			logger.Debugf("checking filename that is standard backup pattern: %s", filename)

			// Parse the date from the filename
			year, month, day, hour, minute, second := matches[1], matches[2], matches[3], matches[4], matches[5], matches[6]
			dateTimeStr := fmt.Sprintf("%s-%s-%sT%s:%s:%sZ", year, month, day, hour, minute, second)
			filetime, err := time.Parse(time.RFC3339, dateTimeStr)
			if err != nil {
				logger.Debugf("Error parsing date from filename %s: %v; ignoring", filename, err)
				continue
			}
			filesWithTimes = append(filesWithTimes, fileWithTime{
				filename: filename,
				filetime: filetime,
			})
		}

		switch {
		case retainHours > 0:
			// if we had retainHours, we go through all of the files and find any whose timestamp is older than now-retainHours
			for _, f := range filesWithTimes {
				// Check if the file is within 'retain' hours from 'now'
				age := now.Sub(f.filetime).Hours()
				if age < float64(retainHours) {
					logger.Debugf("file %s is %f hours old", f.filename, age)
					logger.Debugf("keeping file %s", f.filename)
					continue
				}
				logger.Debugf("Adding candidate file: %s", f.filename)
				candidates = append(candidates, f.filename)
			}
		case retainCount > 0:
			// if we had retainCount, we sort all of the files by timestamp, and add to the list all except the retainCount most recent
			slices.SortFunc(filesWithTimes, func(i, j fileWithTime) int {
				switch {
				case i.filetime.Before(j.filetime):
					return -1
				case i.filetime.After(j.filetime):
					return 1
				}
				return 0
			})
			slices.Reverse(filesWithTimes)
			if retainCount >= len(filesWithTimes) {
				for i := 0 + retainCount; i < len(filesWithTimes); i++ {
					logger.Debugf("Adding candidate file %s:", filesWithTimes[i].filename)
					candidates = append(candidates, filesWithTimes[i].filename)
				}
			}
		default:
			return fmt.Errorf("invalid retention string: %s", opts.Retention)
		}

		// we have the list, remove them all
		for _, filename := range candidates {
			if err := target.Remove(filename, logger); err != nil {
				return fmt.Errorf("failed to remove file %s: %v", filename, err)
			}
			pruned++
		}
		logger.Debugf("pruning %d files from target %s", pruned, target)
	}

	return nil
}

// convertToHours takes a string with format "<integer><unit>" and converts it to hours.
// The unit can be 'h' (hours), 'd' (days), 'w' (weeks), 'm' (months), 'y' (years).
// Assumes 30 days in a month and 365 days in a year for conversion.
func convertToHours(input string) (int, error) {
	re := regexp.MustCompile(`^(\d+)([hdwmy])$`)
	matches := re.FindStringSubmatch(input)

	if matches == nil {
		return 0, fmt.Errorf("invalid format: %s", input)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", matches[1])
	}

	unit := matches[2]
	switch unit {
	case "h":
		return value, nil
	case "d":
		return value * 24, nil
	case "w":
		return value * 24 * 7, nil
	case "m":
		return value * 24 * 30, nil // Approximation
	case "y":
		return value * 24 * 365, nil // Approximation
	default:
		return 0, errors.New("invalid unit")
	}
}

// convertToCount takes a string with format "<integer><unit>" and converts it to count.
// The unit can be 'c' (count)
func convertToCount(input string) (int, error) {
	re := regexp.MustCompile(`^(\d+)([c])$`)
	matches := re.FindStringSubmatch(input)

	if matches == nil {
		return 0, fmt.Errorf("invalid format: %s", input)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", matches[1])
	}

	unit := matches[2]
	switch unit {
	case "c":
		return value, nil
	default:
		return 0, errors.New("invalid unit")
	}
}

type fileWithTime struct {
	filename string
	filetime time.Time
}
