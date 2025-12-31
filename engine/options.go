package engine

import (
	"time"

	"github.com/pico/streamrelay/redis"
)

// Option is a functional option for configuring the Relay
type Option func(*Config)

// WithRedisAddr sets the Redis server address
func WithRedisAddr(addr string) Option {
	return func(c *Config) {
		c.Redis.Addr = addr
	}
}

// WithRedisPassword sets the Redis password
func WithRedisPassword(password string) Option {
	return func(c *Config) {
		c.Redis.Password = password
	}
}

// WithRedisDB sets the Redis database number
func WithRedisDB(db int) Option {
	return func(c *Config) {
		c.Redis.DB = db
	}
}

// WithPrefix sets the prefix for all Redis keys
func WithPrefix(prefix string) Option {
	return func(c *Config) {
		c.Prefix = prefix
	}
}

// WithPoolSize sets the Redis connection pool size
func WithPoolSize(size int) Option {
	return func(c *Config) {
		c.Redis.PoolSize = size
	}
}

// WithDialTimeout sets the Redis dial timeout
func WithDialTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Redis.DialTimeout = timeout
	}
}

// WithReadTimeout sets the Redis read timeout
func WithReadTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Redis.ReadTimeout = timeout
	}
}

// WithWriteTimeout sets the Redis write timeout
func WithWriteTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Redis.WriteTimeout = timeout
	}
}

// WithHistorySize sets the default history size for rooms
func WithHistorySize(size int64) Option {
	return func(c *Config) {
		c.DefaultHistorySize = size
	}
}

// WithHistoryTTL sets the default history TTL for rooms
func WithHistoryTTL(ttl time.Duration) Option {
	return func(c *Config) {
		c.DefaultHistoryTTL = ttl
	}
}

// WithEventHandler sets the event handler
func WithEventHandler(handler EventHandler) Option {
	return func(c *Config) {
		c.EventHandler = handler
	}
}

// WithRedisConfig sets the full Redis configuration
func WithRedisConfig(cfg redis.Config) Option {
	return func(c *Config) {
		c.Redis = cfg
	}
}

// NewWithOptions creates a new Relay with functional options
func NewWithOptions(opts ...Option) (*Relay, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return New(cfg)
}

// RoomOption is a functional option for configuring a Room
type RoomOption func(*RoomConfig)

// WithRoomType sets the room type
func WithRoomType(t RoomType) RoomOption {
	return func(c *RoomConfig) {
		c.Type = t
	}
}

// WithRoomMembers sets the room members
func WithRoomMembers(members ...string) RoomOption {
	return func(c *RoomConfig) {
		c.Members = members
	}
}

// WithRoomHistorySize sets the room's history size
func WithRoomHistorySize(size int64) RoomOption {
	return func(c *RoomConfig) {
		c.HistorySize = size
	}
}

// WithRoomHistoryTTL sets the room's history TTL
func WithRoomHistoryTTL(ttl time.Duration) RoomOption {
	return func(c *RoomConfig) {
		c.HistoryTTL = ttl
	}
}

// NewRoomConfig creates a RoomConfig with functional options
func NewRoomConfig(id string, opts ...RoomOption) RoomConfig {
	cfg := RoomConfig{
		ID:          id,
		Type:        RoomTypeGroup,
		HistorySize: 1000,
		HistoryTTL:  24 * time.Hour,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
