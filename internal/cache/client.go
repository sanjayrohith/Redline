// Package cache wires the gateway's S3-compatible artifact object store.
package cache

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

// UnreferencedTagKey is the object tag the reference-counting layer
// sets to "true" once an artifact's last live deployment goes away.
// The lifecycle policy expires anything so tagged.
const UnreferencedTagKey = "redline-unreferenced"

// DefaultUnreferencedExpiryDays bounds how long an unreferenced artifact
// lingers in the cache before the bucket lifecycle policy reclaims it.
const DefaultUnreferencedExpiryDays = 7

// Options configures a new Client.
type Options struct {
	Endpoint           string
	AccessKeyID        string
	SecretAccessKey    string
	UseSSL             bool
	BucketName         string
	UnreferencedExpiry int // days; DefaultUnreferencedExpiryDays if <= 0
}

// Client wraps a minio.Client bound to one artifact bucket, provisioning
// the bucket and its lifecycle policy on construction.
type Client struct {
	minio    *minio.Client
	core     *minio.Core
	bucket   string
	endpoint string
}

// NewClient connects to an S3-compatible endpoint, creates opts.BucketName
// if it does not already exist, and applies a lifecycle policy expiring
// objects tagged UnreferencedTagKey=true.
func NewClient(ctx context.Context, opts Options) (*Client, error) {
	minioOpts := &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKeyID, opts.SecretAccessKey, ""),
		Secure: opts.UseSSL,
	}

	mc, err := minio.New(opts.Endpoint, minioOpts)
	if err != nil {
		return nil, fmt.Errorf("cache: create minio client: %w", err)
	}

	core, err := minio.NewCore(opts.Endpoint, minioOpts)
	if err != nil {
		return nil, fmt.Errorf("cache: create minio core client: %w", err)
	}

	c := &Client{minio: mc, core: core, bucket: opts.BucketName, endpoint: opts.Endpoint}

	if err := c.ensureBucket(ctx); err != nil {
		return nil, err
	}
	if err := c.applyLifecyclePolicy(ctx, opts.UnreferencedExpiry); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	exists, err := c.minio.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("cache: check bucket %s exists: %w", c.bucket, err)
	}
	if exists {
		return nil
	}

	if err := c.minio.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("cache: create bucket %s: %w", c.bucket, err)
	}
	return nil
}

func (c *Client) applyLifecyclePolicy(ctx context.Context, expiryDays int) error {
	if expiryDays <= 0 {
		expiryDays = DefaultUnreferencedExpiryDays
	}

	cfg := lifecycle.NewConfiguration()
	cfg.Rules = []lifecycle.Rule{
		{
			ID:     "expire-unreferenced-artifacts",
			Status: "Enabled",
			RuleFilter: lifecycle.Filter{
				Tag: lifecycle.Tag{Key: UnreferencedTagKey, Value: "true"},
			},
			Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(expiryDays)},
		},
	}

	if err := c.minio.SetBucketLifecycle(ctx, c.bucket, cfg); err != nil {
		return fmt.Errorf("cache: set lifecycle policy on %s: %w", c.bucket, err)
	}
	return nil
}

// HealthCheck verifies the artifact bucket is reachable and exists.
func (c *Client) HealthCheck(ctx context.Context) error {
	exists, err := c.minio.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("cache: health check: %w", err)
	}
	if !exists {
		return fmt.Errorf("cache: health check: bucket %s does not exist", c.bucket)
	}
	return nil
}

// RemoveObject deletes key from the artifact bucket, such as discarding an
// upload that failed checksum verification.
func (c *Client) RemoveObject(ctx context.Context, key string) error {
	if err := c.minio.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("cache: remove object %s: %w", key, err)
	}
	return nil
}

// quarantinePrefix namespaces objects moved aside for operator inspection
// after failing checksum verification, keeping them out of the normal
// object-key space so nothing else can be misled into serving one.
const quarantinePrefix = "quarantine/"

// Quarantine copies key to a quarantine/-prefixed object (for later
// operator inspection - what actually arrived corrupted, not just that
// something did) and then removes the original key, so nothing can read
// a known-corrupted object from its normal location. It returns the
// quarantined object's key.
func (c *Client) Quarantine(ctx context.Context, key string) (string, error) {
	quarantineKey := quarantinePrefix + key

	src := minio.CopySrcOptions{Bucket: c.bucket, Object: key}
	dst := minio.CopyDestOptions{Bucket: c.bucket, Object: quarantineKey}
	if _, err := c.minio.CopyObject(ctx, dst, src); err != nil {
		return "", fmt.Errorf("cache: copy %s to quarantine: %w", key, err)
	}

	if err := c.RemoveObject(ctx, key); err != nil {
		return "", fmt.Errorf("cache: purge %s after quarantining: %w", key, err)
	}

	return quarantineKey, nil
}

// GetObject opens a streaming reader for key. The caller must close it.
func (c *Client) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.minio.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("cache: get object %s: %w", key, err)
	}
	return obj, nil
}
