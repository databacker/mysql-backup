package log

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/databacker/mysql-backup/pkg/config"
	utiltest "github.com/databacker/mysql-backup/pkg/internal/test"
	"github.com/databacker/mysql-backup/pkg/remote"

	log "github.com/sirupsen/logrus"
)

// TestSendLog tests sending logs. There is no `SendLog` function in the codebase,
// as it is all just a hook for logrus. This test is a test of the actual functionality.
func TestSendLog(t *testing.T) {
	tests := []struct {
		name     string
		level    log.Level
		fields   map[string]interface{}
		bufSize  int
		expected bool
	}{
		{"normal", log.InfoLevel, nil, 1, true},
		{"fatal", log.FatalLevel, nil, 1, true},
		{"error", log.ErrorLevel, nil, 1, true},
		{"warn", log.WarnLevel, nil, 1, true},
		{"debug", log.DebugLevel, nil, 1, true},
		{"debug", log.DebugLevel, nil, 3, true},
		{"trace", log.TraceLevel, nil, 1, false},
		{"self-log", log.InfoLevel, map[string]interface{}{
			sourceField: sourceTelemetry,
		}, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			server, fingerprint, clientKeys, err := utiltest.StartServer(1, func(w http.ResponseWriter, r *http.Request) {
				_, err := buf.ReadFrom(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			})
			if err != nil {
				t.Fatalf("failed to start server: %v", err)
			}
			defer server.Close()

			ch := make(chan int, 1)
			logger := log.New()
			hook, err := NewTelemetry(config.Telemetry{
				Connection: remote.Connection{
					URL:          server.URL,
					Certificates: []string{fingerprint},
					Credentials:  base64.StdEncoding.EncodeToString(clientKeys[0]),
				},
				BufferSize: tt.bufSize,
			}, ch)
			if err != nil {
				t.Fatalf("failed to create telemetry hook: %v", err)
			}
			// add the hook and set the writer
			logger.SetLevel(log.TraceLevel)
			logger.AddHook(hook)
			var localBuf bytes.Buffer
			logger.SetOutput(&localBuf)

			buf.Reset()
			var msgs []string
			for i := 0; i < tt.bufSize; i++ {
				msg := fmt.Sprintf("test message %d random %d", i, rand.Intn(1000))
				msgs = append(msgs, msg)
				logger.WithFields(tt.fields).Log(tt.level, msg)
			}
			// wait for the message to get across, but only one second maximum, as it should be quick
			// this allows us to handle those that should not have a message and never send anything
			var msgCount int
			select {
			case msgCount = <-ch:
			case <-time.After(1 * time.Second):
			}
			if tt.expected {
				if buf.Len() == 0 {
					t.Fatalf("expected log message, got none")
				}
				// message is sent as json, so convert to our structure and compare
				var entries []LogEntry
				if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
					t.Fatalf("failed to unmarshal log entries: %v", err)
				}
				if len(entries) != msgCount {
					t.Fatalf("channel sent %d log entries, actual got %d", msgCount, len(entries))
				}
				if len(entries) != tt.bufSize {
					t.Fatalf("expected %d log entries, got %d", tt.bufSize, len(entries))
				}
				for i, le := range entries {
					if le.Message != msgs[i] {
						t.Errorf("message %d: expected message %q, got %q", i, msgs[i], le.Message)
					}
					if le.Level != tt.level.String() {
						t.Errorf("expected level %q, got %q", tt.level.String(), le.Level)
					}
				}
			} else {
				if buf.Len() != 0 {
					t.Fatalf("expected no log message, got one")
				}
			}
		})
	}
}
