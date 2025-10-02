package cache

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"
)

// MarkUnreferenced tags key as unreferenced, making it eligible for the
// bucket's lifecycle policy (see UnreferencedTagKey in NewClient) to
// expire it after DefaultUnreferencedExpiryDays. Call this only once an
// artifact's live reference count - tracked by the caller, typically
// derived from deployment records - has dropped to zero.
func (c *Client) MarkUnreferenced(ctx context.Context, key string) error {
	t, err := tags.MapToObjectTags(map[string]string{UnreferencedTagKey: "true"})
	if err != nil {
		return fmt.Errorf("cache: build unreferenced tag: %w", err)
	}
	if err := c.minio.PutObjectTagging(ctx, c.bucket, key, t, minio.PutObjectTaggingOptions{}); err != nil {
		return fmt.Errorf("cache: mark %s unreferenced: %w", key, err)
	}
	return nil
}

// MarkReferenced clears any unreferenced tag on key, such as when a new
// deployment starts using a previously-idle cached artifact before the
// lifecycle policy has expired it.
func (c *Client) MarkReferenced(ctx context.Context, key string) error {
	if err := c.minio.RemoveObjectTagging(ctx, c.bucket, key, minio.RemoveObjectTaggingOptions{}); err != nil {
		return fmt.Errorf("cache: mark %s referenced: %w", key, err)
	}
	return nil
}

// IsUnreferenced reports whether key currently carries the unreferenced tag.
func (c *Client) IsUnreferenced(ctx context.Context, key string) (bool, error) {
	t, err := c.minio.GetObjectTagging(ctx, c.bucket, key, minio.GetObjectTaggingOptions{})
	if err != nil {
		return false, fmt.Errorf("cache: get tags for %s: %w", key, err)
	}
	return t.ToMap()[UnreferencedTagKey] == "true", nil
}
