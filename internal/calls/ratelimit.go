package calls

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// CreateLimiter is a simple fixed-window limiter for creating calls.
// Keyed by user_id. Uses Redis INCR + EXPIRE.
type CreateLimiter struct {
	rdb    *redis.Client
	prefix string
	limit  int
	window time.Duration
}

func NewCreateLimiter(rdb *redis.Client, limit int, window time.Duration) *CreateLimiter {
	if limit <= 0 {
		limit = 6
	}
	if window <= 0 {
		window = time.Minute
	}
	return &CreateLimiter{rdb: rdb, prefix: "calls:create:", limit: limit, window: window}
}

func (l *CreateLimiter) Allow(ctx context.Context, userID uuid.UUID) (allowed bool, remaining int, resetIn time.Duration, err error) {
	if l == nil || l.rdb == nil || userID == uuid.Nil {
		return true, l.limit, l.window, nil
	}
	key := fmt.Sprintf("%s%s", l.prefix, userID.String())
	n, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		// fail-open to avoid breaking calls if redis hiccups
		return true, l.limit, l.window, nil
	}
	if n == 1 {
		_ = l.rdb.Expire(ctx, key, l.window).Err()
	}
	ttl, _ := l.rdb.TTL(ctx, key).Result()
	if ttl < 0 {
		ttl = l.window
	}
	remaining = l.limit - int(n)
	if remaining < 0 {
		remaining = 0
	}
	return n <= int64(l.limit), remaining, ttl, nil
}

