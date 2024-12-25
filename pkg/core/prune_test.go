package core

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path"
	"slices"
	"testing"
	"time"

	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/storage/credentials"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestConvertToHours(t *testing.T) {
	tests := []struct {
		input  string
		output int
		err    error
	}{
		{"2h", 2, nil},
		{"3w", 3 * 7 * 24, nil},
		{"5d", 5 * 24, nil},
		{"1m", 30 * 24, nil},
		{"1y", 365 * 24, nil},
		{"100x", 0, fmt.Errorf("invalid format: 100x")},
	}
	for _, tt := range tests {
		hours, err := convertToHours(tt.input)
		switch {
		case (err == nil && tt.err != nil) || (err != nil && tt.err == nil):
			t.Errorf("expected error %v, got %v", tt.err, err)
		case err != nil && tt.err != nil && err.Error() != tt.err.Error():
			t.Errorf("expected error %v, got %v", tt.err, err)
		case hours != tt.output:
			t.Errorf("input %s expected %d, got %d", tt.input, tt.output, hours)
		}
	}
}

func TestPrune(t *testing.T) {
	// we use a fixed list of file before, and a subset of them for after
	// db_backup_YYYY-MM-DDTHH:mm:ssZ.<compression>
	// our list of timestamps should give us these files, of the following time ago:
	// 0.25h, 1h, 2h, 3h, 24h (1d), 36h (1.5d), 48h (2d), 60h (2.5d) 72h(3d),
	// 167h (1w-1h), 168h (1w), 240h (1.5w) 336h (2w), 576h (2.5w), 504h (3w)
	// 744h (3.5w), 720h (1m), 1000h (1.5m), 1440h (2m), 1800h (2.5m), 2160h (3m),
	// 8760h (1y), 12000h (1.5y), 17520h (2y)
	// we use a fixed starting time to make it consistent.
	now := time.Date(2021, 1, 1, 0, 30, 0, 0, time.UTC)
	hoursAgo := []float32{0.25, 1, 2, 3, 24, 36, 48, 60, 72, 167, 168, 240, 336, 504, 576, 744, 720, 1000, 1440, 1800, 2160, 8760, 12000, 17520}
	// convert to filenames
	var filenames, safefilenames []string
	for _, h := range hoursAgo {
		// convert the time diff into a duration, do not forget the negative
		duration, err := time.ParseDuration(fmt.Sprintf("-%fh", h))
		if err != nil {
			t.Fatalf("failed to parse duration: %v", err)
		}
		// convert it into a time.Time
		// and add 30 mins to our "now" time.
		relativeTime := now.Add(duration).Add(-30 * time.Minute)
		// convert that into the filename
		filename := fmt.Sprintf("db_backup_%sZ.gz", relativeTime.Format("2006-01-02T15:04:05"))
		filenames = append(filenames, filename)
		safefilename := fmt.Sprintf("db_backup_%sZ.gz", relativeTime.Format("2006-01-02T15-04-05"))
		safefilenames = append(safefilenames, safefilename)
	}
	tests := []struct {
		name        string
		opts        PruneOptions
		beforeFiles []string
		afterFiles  []string
		err         error
	}{
		{"no targets", PruneOptions{Retention: "1h", Now: now}, nil, nil, fmt.Errorf("no targets")},
		{"invalid format", PruneOptions{Retention: "100x", Now: now}, filenames, filenames[0:1], fmt.Errorf("invalid retention string: 100x")},
		// 1 hour - file[1] is 1h+30m = 1.5h, so it should be pruned
		{"1 hour", PruneOptions{Retention: "1h", Now: now}, filenames, filenames[0:1], nil},
		// 2 hours - file[2] is 2h+30m = 2.5h, so it should be pruned
		{"2 hours", PruneOptions{Retention: "2h", Now: now}, filenames, filenames[0:2], nil},
		// 2 days - file[6] is 48h+30m = 48.5h, so it should be pruned
		{"2 days", PruneOptions{Retention: "2d", Now: now}, filenames, filenames[0:6], nil},
		// 3 weeks - file[13] is 504h+30m = 504.5h, so it should be pruned
		{"3 weeks", PruneOptions{Retention: "3w", Now: now}, filenames, filenames[0:13], nil},
		// 2 most recent files
		{"2 most recent", PruneOptions{Retention: "2c", Now: now}, filenames, filenames[0:2], nil},

		// repeat for safe file names
		{"1 hour safe names", PruneOptions{Retention: "1h", Now: now}, safefilenames, safefilenames[0:1], nil},
		// 2 hours - file[2] is 2h+30m = 2.5h, so it should be pruned
		{"2 hours safe names", PruneOptions{Retention: "2h", Now: now}, safefilenames, safefilenames[0:2], nil},
		// 2 days - file[6] is 48h+30m = 48.5h, so it should be pruned
		{"2 days safe names", PruneOptions{Retention: "2d", Now: now}, safefilenames, safefilenames[0:6], nil},
		// 3 weeks - file[13] is 504h+30m = 504.5h, so it should be pruned
		{"3 weeks safe names", PruneOptions{Retention: "3w", Now: now}, safefilenames, safefilenames[0:13], nil},
		// 2 most recent files
		{"2 most recent safe names", PruneOptions{Retention: "2c", Now: now}, safefilenames, safefilenames[0:2], nil},
	}
	for _, targetType := range []string{"file", "s3"} {
		t.Run(targetType, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					ctx := context.Background()
					logger := log.New()
					logger.Out = io.Discard
					// create a temporary directory
					// create beforeFiles in the directory and create a target, but only if there are beforeFiles
					// this lets us also test no targets, which should generate an error
					if len(tt.beforeFiles) > 0 {
						var (
							store storage.Storage
							err   error
						)
						switch targetType {
						case "file":
							// add our tempdir as the target
							workDir := t.TempDir()
							store, err = storage.ParseURL(fmt.Sprintf("file://%s", workDir), credentials.Creds{})
							if err != nil {
								t.Errorf("failed to parse file url: %v", err)
								return
							}
						case "s3":
							bucketName := "mytestbucket"
							s3backend := s3mem.New()
							// create the bucket we will use for tests
							if err := s3backend.CreateBucket(bucketName); err != nil {
								t.Errorf("failed to create bucket: %v", err)
								return
							}
							s3 := gofakes3.New(s3backend)
							s3server := httptest.NewServer(s3.Server())
							defer s3server.Close()
							s3url := s3server.URL
							store, err = storage.ParseURL(fmt.Sprintf("s3://%s/%s", bucketName, bucketName), credentials.Creds{AWS: credentials.AWSCreds{
								Endpoint:        s3url,
								AccessKeyID:     "abcdefg",
								SecretAccessKey: "1234567",
								Region:          "us-east-1",
								PathStyle:       true,
							}})
							if err != nil {
								t.Errorf("failed to parse s3 url: %v", err)
								return
							}
						default:
							t.Errorf("unknown target type: %s", targetType)
							return
						}

						tt.opts.Targets = append(tt.opts.Targets, store)

						for _, filename := range tt.beforeFiles {
							// we need an empty file to push
							srcDir := t.TempDir()
							srcFile := fmt.Sprintf("%s/%s", srcDir, "src")
							if err := os.WriteFile(srcFile, nil, 0644); err != nil {
								t.Errorf("failed to create file %s: %v", srcFile, err)
								return
							}

							// now push that same empty file each time; we do not care about contents, only that the target file exists
							if _, err := store.Push(ctx, filename, srcFile, log.NewEntry(logger)); err != nil {
								t.Errorf("failed to create file %s: %v", filename, err)
								return
							}
						}
					}

					// run Prune
					executor := Executor{
						Logger: logger,
					}
					err := executor.Prune(ctx, tt.opts)
					switch {
					case (err == nil && tt.err != nil) || (err != nil && tt.err == nil):
						t.Errorf("expected error %v, got %v", tt.err, err)
					case err != nil && tt.err != nil && err.Error() != tt.err.Error():
						t.Errorf("expected error %v, got %v", tt.err, err)
					case err != nil:
						return
					}
					// check files match
					files, err := tt.opts.Targets[0].ReadDir(ctx, "", log.NewEntry(logger))
					if err != nil {
						t.Errorf("failed to read directory: %v", err)
						return
					}
					var afterFiles []string
					for _, file := range files {
						afterFiles = append(afterFiles, path.Base(file.Name()))
					}
					afterFilesSorted, ttAfterFilesSorted := slices.Clone(afterFiles), slices.Clone(tt.afterFiles)
					slices.Sort(afterFilesSorted)
					slices.Sort(ttAfterFilesSorted)
					assert.ElementsMatch(t, ttAfterFilesSorted, afterFilesSorted)
				})
			}
		})
	}
}
