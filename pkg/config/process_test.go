package config

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"testing"

	utiltest "github.com/databacker/mysql-backup/pkg/internal/test"
	"gopkg.in/yaml.v3"

	"github.com/databacker/api/go/api"
	"github.com/google/go-cmp/cmp"
)

func TestGetRemoteConfig(t *testing.T) {
	configFile := "./testdata/config.yml"
	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	var validConfig api.Config
	if err := yaml.Unmarshal(content, &validConfig); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	// start the server before the tests
	server, fingerprint, clientKeys, err := utiltest.StartServer(1, func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		f, err := os.Open(configFile)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		if _, err = buf.ReadFrom(f); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		if _, err := w.Write(buf.Bytes()); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Close()
	tests := []struct {
		name   string
		url    string
		err    string
		config api.Config
	}{
		{"no url", "", "unsupported protocol scheme", api.Config{}},
		{"invalid server", "https://foo.bar/com", "no such host", api.Config{}},
		{"no path", "https://google.com/foo/bar/abc", "invalid config file", api.Config{}},
		{"nothing listening", "https://localhost:12345/foo/bar/abc", "connection refused", api.Config{}},
		{"valid", server.URL, "", validConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds := base64.StdEncoding.EncodeToString(clientKeys[0])
			spec := api.RemoteSpec{
				URL:          &tt.url,
				Certificates: &[]string{fingerprint},
				Credentials:  &creds,
			}
			conf, err := getRemoteConfig(spec)
			switch {
			case tt.err == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.err != "" && err == nil:
				t.Fatalf("expected error: %s", tt.err)
			case tt.err != "" && !strings.Contains(err.Error(), tt.err):
				t.Fatalf("mismatched error: %s, got: %v", tt.err, err)
			default:
				diff := cmp.Diff(tt.config, conf)
				if diff != "" {
					t.Fatalf("mismatched config: %s", diff)
				}
			}
		})
	}

}
