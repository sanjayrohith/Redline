package cache

import (
	"context"
	"testing"
	"time"

	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
)

func newTestClient(t *testing.T, bucket string) *Client {
	t.Helper()
	ctx := context.Background()

	const user, password = "minioadmin", "minioadmin"

	container, err := tcminio.Run(ctx, "minio/minio:latest",
		tcminio.WithUsername(user),
		tcminio.WithPassword(password),
	)
	if err != nil {
		t.Skipf("skipping integration test: could not start minio container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	newCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := NewClient(newCtx, Options{
		Endpoint:        endpoint,
		AccessKeyID:     user,
		SecretAccessKey: password,
		UseSSL:          false,
		BucketName:      bucket,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func TestNewClient_ProvisionsBucketAndLifecycle(t *testing.T) {
	client := newTestClient(t, "test-artifacts-provision")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	cfg, err := client.minio.GetBucketLifecycle(ctx, client.bucket)
	if err != nil {
		t.Fatalf("GetBucketLifecycle() error = %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("len(Rules) = %d, want 1", len(cfg.Rules))
	}
	if cfg.Rules[0].RuleFilter.Tag.Key != UnreferencedTagKey {
		t.Errorf("lifecycle tag key = %q, want %q", cfg.Rules[0].RuleFilter.Tag.Key, UnreferencedTagKey)
	}
}

func TestNewClient_IdempotentOnExistingBucket(t *testing.T) {
	client := newTestClient(t, "test-artifacts-idempotent")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := NewClient(ctx, Options{
		Endpoint:        client.endpoint,
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		BucketName:      client.bucket,
	}); err != nil {
		t.Fatalf("second NewClient() error = %v, want nil (bucket already exists)", err)
	}
}

func TestHealthCheck_MissingBucket(t *testing.T) {
	client := newTestClient(t, "test-artifacts-healthcheck-source")

	// Point HealthCheck at a bucket that was never provisioned.
	client.bucket = "this-bucket-does-not-exist-at-all"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("HealthCheck() error = nil, want error for a missing bucket")
	}
}
