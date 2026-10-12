package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DragonflyClient owns the two stateful guarantees the API advertises: 24 hour
// idempotency on application submissions and a fixed-window rate limit. Both
// live in DragonflyDB so they survive an API restart and hold across replicas.
type DragonflyClient struct {
	rdb *redis.Client
}

func NewDragonflyClient(url string) (*DragonflyClient, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse DRAGONFLY_URL: %w", err)
	}
	c := &DragonflyClient{rdb: redis.NewClient(opts)}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		_ = c.rdb.Close()
		return nil, fmt.Errorf("ping dragonfly: %w", err)
	}
	return c, nil
}

func (c *DragonflyClient) Close() error { return c.rdb.Close() }

func (c *DragonflyClient) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// ClaimIdempotency returns true when this caller owns the key. A duplicate
// submission inside the window gets false and must be answered with the
// replayed response rather than a second application.
func (c *DragonflyClient) ClaimIdempotency(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, "idemp:claim:"+key, "IN_PROGRESS", ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ReplayIdempotency returns the stored response for a previously seen key.
func (c *DragonflyClient) ReplayIdempotency(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := c.rdb.Get(ctx, "idemp:resp:"+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return val, true, nil
}

func (c *DragonflyClient) SaveIdempotency(ctx context.Context, key string, response []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "idemp:resp:"+key, response, ttl).Err()
}

// ReleaseIdempotency drops a claim so a failed request can be retried by the
// client instead of being locked out for the whole window.
func (c *DragonflyClient) ReleaseIdempotency(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, "idemp:claim:"+key).Err()
}

// AllowRequest implements a fixed-window counter. limit must be >= 1.
func (c *DragonflyClient) AllowRequest(ctx context.Context, bucket string, limit int, window time.Duration) (bool, error) {
	key := "ratelimit:" + bucket
	count, err := c.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := c.rdb.Expire(ctx, key, window).Err(); err != nil {
			return false, err
		}
	}
	return count <= int64(limit), nil
}
