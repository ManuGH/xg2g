// SPDX-License-Identifier: MIT

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// RedisCache is a Redis-backed implementation of Cache.
type RedisCache struct {
	client *redis.Client
	logger zerolog.Logger
	stats  struct {
		hits      atomic.Int64
		misses    atomic.Int64
		sets      atomic.Int64
		evictions atomic.Int64
	}
}

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	Addr     string // Redis server address (host:port)
	Password string // Redis password (optional)
	DB       int    // Redis database number
}

// NewRedisCache creates a new Redis-backed cache.
// Falls back to in-memory cache if Redis is unavailable.
func NewRedisCache(config RedisConfig, logger zerolog.Logger) (Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         config.Addr,
		Password:     config.Password,
		DB:           config.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	logger.Info().
		Str("addr", config.Addr).
		Int("db", config.DB).
		Msg("connected to Redis cache")

	return &RedisCache{
		client: client,
		logger: logger,
	}, nil
}

// Get retrieves a value from Redis cache.
func (c *RedisCache) Get(key string) (any, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		c.stats.misses.Add(1)
		return nil, false
	}
	if err != nil {
		c.logger.Warn().Err(err).Str("key", key).Msg("redis get failed")
		c.stats.misses.Add(1)
		return nil, false
	}

	// Deserialize JSON
	var result any
	if err := json.Unmarshal(val, &result); err != nil {
		c.logger.Warn().Err(err).Str("key", key).Msg("json unmarshal failed")
		c.stats.misses.Add(1)
		return nil, false
	}

	c.stats.hits.Add(1)
	return result, true
}

// Set stores a value in Redis cache with TTL.
func (c *RedisCache) Set(key string, value any, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Serialize to JSON
	data, err := json.Marshal(value)
	if err != nil {
		c.logger.Warn().Err(err).Str("key", key).Msg("json marshal failed")
		return
	}

	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		c.logger.Warn().Err(err).Str("key", key).Msg("redis set failed")
		return
	}

	c.stats.sets.Add(1)
}

// Delete removes a value from Redis cache.
func (c *RedisCache) Delete(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.client.Del(ctx, key).Err(); err != nil {
		c.logger.Warn().Err(err).Str("key", key).Msg("redis delete failed")
	}
}

// Clear removes all values from the cache (flushes current DB).
func (c *RedisCache) Clear() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.client.FlushDB(ctx).Err(); err != nil {
		c.logger.Warn().Err(err).Msg("redis flush failed")
	}
}

// Stats returns cache statistics.
func (c *RedisCache) Stats() CacheStats {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get current size from Redis
	size, err := c.client.DBSize(ctx).Result()
	if err != nil {
		c.logger.Warn().Err(err).Msg("redis dbsize failed")
		size = 0
	}

	return CacheStats{
		Hits:        c.stats.hits.Load(),
		Misses:      c.stats.misses.Load(),
		Sets:        c.stats.sets.Load(),
		Evictions:   c.stats.evictions.Load(),
		CurrentSize: int(size),
	}
}

// Close closes the Redis connection.
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// HealthCheck checks if Redis is available.
func (c *RedisCache) HealthCheck(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}
