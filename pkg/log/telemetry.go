package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/databacker/mysql-backup/pkg/config"
	"github.com/databacker/mysql-backup/pkg/remote"
	log "github.com/sirupsen/logrus"
)

const (
	sourceField     = "source"
	sourceTelemetry = "telemetry"
)

// NewTelemetry creates a new telemetry writer, which writes to the configured telemetry endpoint.
// NewTelemetry creates an initial connection, which it keeps open and then can reopen as needed for each write.
func NewTelemetry(conf config.Telemetry, ch chan<- int) (log.Hook, error) {
	client, err := remote.GetTLSClient(conf.Certificates, conf.Credentials)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, conf.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// GET the telemetry endpoint; this is just done to check that it is valid.
	// Other requests will be POSTs.
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error requesting telemetry endpoint: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error requesting telemetry endpoint: %s", resp.Status)
	}
	return &telemetry{conf: conf, client: client, ch: ch}, nil
}

type telemetry struct {
	conf   config.Telemetry
	client *http.Client
	buffer []*log.Entry
	// ch channel to indicate when done sending a message, in case needed for synchronization, e.g. testing.
	// sends a count down the channel when done sending a message to the remote. The count is the number.
	// of messages sent.
	ch chan<- int
}

// Levels the levels for which the hook should fire
func (t *telemetry) Levels() []log.Level {
	return []log.Level{log.PanicLevel, log.FatalLevel, log.ErrorLevel, log.WarnLevel, log.InfoLevel, log.DebugLevel}
}

// Fire send off a log entry.
func (t *telemetry) Fire(entry *log.Entry) error {
	// send the log entry to the telemetry endpoint
	// this is blocking, and we do not want to do so, so do it in a go routine
	// and do not wait for the response.

	// if this message is from ourself, do not try to send it again
	if entry.Data[sourceField] == sourceTelemetry {
		return nil
	}
	t.buffer = append(t.buffer, entry)
	if t.conf.BufferSize <= 1 || len(t.buffer) >= t.conf.BufferSize {
		entries := t.buffer
		t.buffer = nil
		go func(entries []*log.Entry, ch chan<- int) {
			if ch != nil {
				defer func() { ch <- len(entries) }()
			}
			l := entry.Logger.WithField(sourceField, sourceTelemetry)
			l.Level = entry.Logger.Level
			remoteEntries := make([]LogEntry, len(entries))
			for i, entry := range entries {
				// send the structured data to the telemetry endpoint
				var runID string
				if v, ok := entry.Data["run"]; ok {
					runID = v.(string)
				}
				remoteEntries[i] = LogEntry{
					Run:       runID,
					Timestamp: entry.Time.Format("2006-01-02T15:04:05.000Z"),
					Level:     entry.Level.String(),
					Fields:    entry.Data,
					Message:   entry.Message,
				}
			}
			// marshal to json
			b, err := json.Marshal(remoteEntries)
			if err != nil {
				l.Errorf("error marshalling log entry: %v", err)
				return
			}
			req, err := http.NewRequest(http.MethodPost, t.conf.URL, bytes.NewReader(b))
			if err != nil {
				l.Errorf("error creating telemetry HTTP request: %v", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			// POST to the telemetry endpoint
			resp, err := t.client.Do(req)
			if err != nil {
				l.Errorf("error connecting to telemetry endpoint: %v", err)
				return
			}

			if resp.StatusCode != http.StatusCreated {
				l.Errorf("failed sending data telemetry endpoint: %s", resp.Status)
				return
			}
		}(entries, t.ch)
	}
	return nil
}
