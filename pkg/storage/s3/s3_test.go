package s3

import (
	"bytes"
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	log "github.com/sirupsen/logrus"
)

func TestPullFromPathInBucket(t *testing.T) {
	const (
		bucket  = "backups"
		dumpKey = "prod/db_backup.tgz"
	)
	content := []byte("dump contents")

	backend := s3mem.New()
	if err := backend.CreateBucket(bucket); err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}
	if _, err := backend.PutObject(bucket, dumpKey, map[string]string{}, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("failed to put object: %v", err)
	}
	server := httptest.NewServer(gofakes3.New(backend).Server())
	defer server.Close()

	handler := New(url.URL{Scheme: "s3", Host: bucket, Path: "/prod"},
		WithEndpoint(server.URL),
		WithPathStyle(),
		WithRegion("us-east-1"),
		WithAccessKeyId("key"),
		WithSecretAccessKey("secret"),
	)
	target := filepath.Join(t.TempDir(), "db_backup.tgz")

	n, err := handler.Pull(context.Background(), "db_backup.tgz", target, log.NewEntry(log.New()))
	if err != nil {
		t.Fatalf("failed to pull: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("pulled %d bytes instead of expected %d", n, len(content))
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read pulled file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("pulled content mismatch: got %q, want %q", got, content)
	}
}
