package objectstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Store struct {
	cfg    Config
	client *s3.Client
}

func New(cfg Config) (*Store, error) {
	if !cfg.Enabled {
		return &Store{cfg: cfg}, nil
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("DEBRIDNEST_S3_BUCKET is required when S3 is enabled")
	}

	opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = cfg.ForcePathStyle
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		},
	}

	var awsCfg aws.Config
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		awsCfg = aws.Config{
			Region:      cfg.Region,
			Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		}
	} else {
		awsCfg = aws.Config{Region: cfg.Region}
	}

	client := s3.NewFromConfig(awsCfg, opts...)
	return &Store{cfg: cfg, client: client}, nil
}

func (s *Store) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.client != nil
}

func (s *Store) OffloadLocal() bool {
	return s != nil && s.cfg.OffloadLocal
}

// TestConnection verifies bucket access via HeadBucket, falling back to ListObjectsV2.
func (s *Store) TestConnection(ctx context.Context) error {
	if !s.Enabled() {
		return fmt.Errorf("object store disabled")
	}
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.cfg.Bucket),
	})
	if err == nil {
		return nil
	}

	_, err = s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.cfg.Bucket),
		MaxKeys: aws.Int32(1),
		Prefix:  aws.String(s.cfg.Prefix),
	})
	return err
}

func (s *Store) ObjectKey(infoHash, filePath string) string {
	filePath = strings.ReplaceAll(filePath, "\\", "/")
	filePath = filepath.ToSlash(filePath)
	filePath = strings.Trim(filePath, "/")
	for strings.Contains(filePath, "//") {
		filePath = strings.ReplaceAll(filePath, "//", "/")
	}
	parts := []string{}
	if s.cfg.Prefix != "" {
		parts = append(parts, s.cfg.Prefix)
	}
	parts = append(parts, strings.ToLower(infoHash), filePath)
	return strings.Join(parts, "/")
}

func (s *Store) Upload(ctx context.Context, localPath, key string) error {
	if !s.Enabled() {
		return fmt.Errorf("object store disabled")
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.cfg.Bucket),
		Key:           aws.String(key),
		Body:          f,
		ContentLength: aws.Int64(st.Size()),
	})
	return err
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if !s.Enabled() {
		return fmt.Errorf("object store disabled")
	}
	if key == "" {
		return nil
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *Store) Head(ctx context.Context, key string) (size int64, modTime time.Time, err error) {
	if !s.Enabled() {
		return 0, time.Time{}, fmt.Errorf("object store disabled")
	}
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	if out.LastModified != nil {
		modTime = *out.LastModified
	}
	return size, modTime, nil
}

func (s *Store) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("object store disabled")
	}
	size, _, err := s.Head(ctx, key)
	if err != nil {
		return nil, err
	}
	return newRangeReader(s, ctx, key, size), nil
}

func (s *Store) OpenRange(ctx context.Context, key string, start, length int64) (io.ReadCloser, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("object store disabled")
	}
	if length <= 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	end := start + length - 1
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", start, end)),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

type rangeReader struct {
	store *Store
	ctx   context.Context
	key   string
	size  int64
	pos   int64
	rc    io.ReadCloser
	mu    sync.Mutex
}

func newRangeReader(store *Store, ctx context.Context, key string, size int64) *rangeReader {
	return &rangeReader{store: store, ctx: ctx, key: key, size: size}
}

func (r *rangeReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pos >= r.size {
		return 0, io.EOF
	}
	if r.rc == nil {
		if err := r.openRangeLocked(r.pos, r.size-r.pos); err != nil {
			return 0, err
		}
	}

	n, err := r.rc.Read(p)
	r.pos += int64(n)
	if err == io.EOF && r.pos < r.size {
		_ = r.rc.Close()
		r.rc = nil
		if err := r.openRangeLocked(r.pos, r.size-r.pos); err != nil {
			return n, err
		}
		return n, nil
	}
	return n, err
}

func (r *rangeReader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		abs = r.size + offset
	default:
		return 0, fmt.Errorf("invalid whence")
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative position")
	}
	if abs > r.size {
		abs = r.size
	}

	if r.rc != nil {
		_ = r.rc.Close()
		r.rc = nil
	}
	r.pos = abs
	return r.pos, nil
}

func (r *rangeReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rc == nil {
		return nil
	}
	err := r.rc.Close()
	r.rc = nil
	return err
}

func (r *rangeReader) openRangeLocked(start, length int64) error {
	if length <= 0 {
		return nil
	}
	rc, err := r.store.OpenRange(r.ctx, r.key, start, length)
	if err != nil {
		return err
	}
	r.rc = rc
	return nil
}
