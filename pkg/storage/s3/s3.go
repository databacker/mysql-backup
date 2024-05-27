package s3

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	log "github.com/sirupsen/logrus"
)

type S3 struct {
	url url.URL
	// pathStyle option is not really used, but may be required
	// at some point; see https://aws.amazon.com/blogs/aws/amazon-s3-path-deprecation-plan-the-rest-of-the-story/
	pathStyle       bool
	region          string
	endpoint        string
	accessKeyId     string
	secretAccessKey string
}

type Option func(s *S3)

func WithPathStyle() Option {
	return func(s *S3) {
		s.pathStyle = true
	}
}
func WithRegion(region string) Option {
	return func(s *S3) {
		s.region = region
	}
}
func WithEndpoint(endpoint string) Option {
	return func(s *S3) {
		s.endpoint = endpoint
	}
}
func WithAccessKeyId(accessKeyId string) Option {
	return func(s *S3) {
		s.accessKeyId = accessKeyId
	}
}
func WithSecretAccessKey(secretAccessKey string) Option {
	return func(s *S3) {
		s.secretAccessKey = secretAccessKey
	}
}

func New(u url.URL, opts ...Option) *S3 {
	s := &S3{url: u}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *S3) Pull(ctx context.Context, source, target string, logger *log.Entry) (int64, error) {
	// get the s3 client
	client, err := s.getClient(logger)
	if err != nil {
		return 0, fmt.Errorf("failed to get AWS client: %v", err)
	}

	bucket, path := s.url.Hostname(), path.Join(s.url.Path, source)

	// Create a downloader with the session and default options
	downloader := manager.NewDownloader(client)

	// Create a file to write the S3 Object contents to.
	f, err := os.Create(target)
	if err != nil {
		return 0, fmt.Errorf("failed to create target restore file %q, %v", target, err)
	}
	defer f.Close()

	// Write the contents of S3 Object to the file
	n, err := downloader.Download(context.TODO(), f, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to download file, %v", err)
	}
	return n, nil
}

func (s *S3) Push(ctx context.Context, target, source string, logger *log.Entry) (int64, error) {
	// get the s3 client
	client, err := s.getClient(logger)
	if err != nil {
		return 0, fmt.Errorf("failed to get AWS client: %v", err)
	}
	bucket, key := s.url.Hostname(), s.url.Path

	// Create an uploader with the session and default options
	uploader := manager.NewUploader(client)

	// Create a file to write the S3 Object contents to.
	f, err := os.Open(source)
	if err != nil {
		return 0, fmt.Errorf("failed to read input file %q, %v", source, err)
	}
	defer f.Close()
	countingReader := NewCountingReader(f)

	// S3 always prepends a /, so if it already has one, it would become //
	// For some services, that is ok, but for others, it causes issues.
	key = strings.TrimPrefix(path.Join(key, target), "/")

	// Write the contents of the file to the S3 object
	_, err = uploader.Upload(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   countingReader,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload file, %v", err)
	}
	return countingReader.Bytes(), nil
}

func (s *S3) Clean(filename string) string {
	return filename
}

func (s *S3) Protocol() string {
	return "s3"
}

func (s *S3) URL() string {
	return s.url.String()
}

func (s *S3) ReadDir(ctx context.Context, dirname string, logger *log.Entry) ([]fs.FileInfo, error) {
	// get the s3 client
	client, err := s.getClient(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get AWS client: %v", err)
	}

	// Call ListObjectsV2 with your bucket and prefix
	result, err := client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{Bucket: aws.String(s.url.Hostname()), Prefix: aws.String(dirname)})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects, %v", err)
	}

	// Convert s3.Object to fs.FileInfo
	var files []fs.FileInfo
	for _, item := range result.Contents {
		files = append(files, &s3FileInfo{
			name:         *item.Key,
			lastModified: *item.LastModified,
			size:         *item.Size,
		})
	}

	return files, nil
}

func (s *S3) Remove(ctx context.Context, target string, logger *log.Entry) error {
	// Get the AWS client
	client, err := s.getClient(logger)
	if err != nil {
		return fmt.Errorf("failed to get AWS client: %v", err)
	}

	// Call DeleteObject with your bucket and the key of the object you want to delete
	_, err = client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.url.Hostname()),
		Key:    aws.String(target),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object, %v", err)
	}

	return nil
}

func (s *S3) getClient(logger *log.Entry) (*s3.Client, error) {
	// Get the AWS config
	var configOpts []func(*config.LoadOptions) error // global client options
	if logger.Level == log.TraceLevel {
		configOpts = append(configOpts, config.WithClientLogMode(aws.LogRequestWithBody|aws.LogResponse))
	}
	if s.region != "" {
		configOpts = append(configOpts, config.WithRegion(s.region))
	}
	if s.accessKeyId != "" {
		configOpts = append(configOpts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			s.accessKeyId,
			s.secretAccessKey,
			"",
		)))
	}
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		configOpts...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %v", err)
	}

	// Get the S3 client
	var s3opts []func(*s3.Options) // s3 client options
	if s.endpoint != "" {
		cleanEndpoint := getEndpoint(s.endpoint)
		s3opts = append(s3opts,
			func(o *s3.Options) {
				o.BaseEndpoint = &cleanEndpoint
			},
		)
	}
	if s.pathStyle {
		s3opts = append(s3opts,
			func(o *s3.Options) {
				o.UsePathStyle = true
			},
		)
	}

	// Create a new S3 service client
	return s3.NewFromConfig(cfg, s3opts...), nil
}

// getEndpoint returns a clean (for AWS client) endpoint. Normally, this is unchanged,
// but for some reason, the lookup gets flaky when the endpoint is 127.0.0.1,
// so in that case, set it to localhost explicitly.
func getEndpoint(endpoint string) string {
	e := endpoint
	u, err := url.Parse(endpoint)
	if err == nil {
		if u.Hostname() == "127.0.0.1" {
			port := u.Port()
			u.Host = "localhost"
			if port != "" {
				u.Host += ":" + port
			}
			e = u.String()
		}
	}
	return e
}

type s3FileInfo struct {
	name         string
	lastModified time.Time
	size         int64
}

func (s s3FileInfo) Name() string       { return s.name }
func (s s3FileInfo) Size() int64        { return s.size }
func (s s3FileInfo) Mode() os.FileMode  { return 0 } // Not applicable in S3
func (s s3FileInfo) ModTime() time.Time { return s.lastModified }
func (s s3FileInfo) IsDir() bool        { return false } // Not applicable in S3
func (s s3FileInfo) Sys() interface{}   { return nil }   // Not applicable in S3
