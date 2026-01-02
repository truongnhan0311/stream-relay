package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

var (
	ErrClosed        = errors.New("connection closed")
	ErrTimeout       = errors.New("operation timeout")
	ErrAuthFailed    = errors.New("authentication failed")
	ErrPoolExhausted = errors.New("connection pool exhausted")
)

// Config holds Redis connection configuration
type Config struct {
	// Addr is the Redis server address (host:port)
	Addr string

	// Password for Redis AUTH command (empty for no auth)
	Password string

	// DB is the database number to SELECT
	DB int

	// DialTimeout is the timeout for establishing connection
	DialTimeout time.Duration

	// ReadTimeout is the timeout for read operations
	ReadTimeout time.Duration

	// WriteTimeout is the timeout for write operations
	WriteTimeout time.Duration

	// PoolSize is the maximum number of connections in the pool
	PoolSize int

	// MinIdleConns is the minimum number of idle connections
	MinIdleConns int
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
	}
}

// Conn represents a single Redis connection
type Conn struct {
	conn   net.Conn
	writer *Writer
	reader *Reader
	mu     sync.Mutex
	closed bool
}

// newConn creates a new connection to Redis
func newConn(cfg Config) (*Conn, error) {
	// Dial with timeout
	dialer := net.Dialer{Timeout: cfg.DialTimeout}
	netConn, err := dialer.Dial("tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	c := &Conn{
		conn:   netConn,
		writer: NewWriter(netConn),
		reader: NewReader(netConn),
	}

	// Authenticate if password is set
	if cfg.Password != "" {
		if err := c.auth(cfg.Password); err != nil {
			c.Close()
			return nil, err
		}
	}

	// Select database if not 0
	if cfg.DB != 0 {
		if err := c.selectDB(cfg.DB); err != nil {
			c.Close()
			return nil, err
		}
	}

	return c, nil
}

// auth sends AUTH command
func (c *Conn) auth(password string) error {
	resp, err := c.Do("AUTH", password)
	if err != nil {
		return err
	}
	if resp.IsError {
		return fmt.Errorf("%w: %s", ErrAuthFailed, resp.Str)
	}
	return nil
}

// selectDB sends SELECT command
func (c *Conn) selectDB(db int) error {
	resp, err := c.Do("SELECT", fmt.Sprintf("%d", db))
	if err != nil {
		return err
	}
	if resp.IsError {
		return fmt.Errorf("SELECT failed: %s", resp.Str)
	}
	return nil
}

// Do executes a Redis command and returns the response
func (c *Conn) Do(args ...string) (Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return Value{}, ErrClosed
	}

	// Write command
	if err := c.writer.WriteCommand(args...); err != nil {
		return Value{}, fmt.Errorf("write error: %w", err)
	}

	// Read response
	return c.reader.ReadValue()
}

// Close closes the connection
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}

// IsClosed returns whether the connection is closed
func (c *Conn) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Pool is a connection pool for Redis
type Pool struct {
	cfg     Config
	conns   chan *Conn
	mu      sync.Mutex
	closed  bool
	created int
}

// NewPool creates a new connection pool
func NewPool(cfg Config) (*Pool, error) {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 10
	}

	p := &Pool{
		cfg:   cfg,
		conns: make(chan *Conn, cfg.PoolSize),
	}

	// Pre-create minimum idle connections
	for i := 0; i < cfg.MinIdleConns && i < cfg.PoolSize; i++ {
		conn, err := newConn(cfg)
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("failed to create initial connection: %w", err)
		}
		p.conns <- conn
		p.created++
	}

	return p, nil
}

// Get retrieves a connection from the pool
func (p *Pool) Get(ctx context.Context) (*Conn, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrClosed
	}
	p.mu.Unlock()

	// Try to get from pool first
	select {
	case conn := <-p.conns:
		if conn.IsClosed() {
			// Connection is dead, create new one
			return p.createConn()
		}
		return conn, nil
	default:
		// Pool is empty
	}

	// Try to create new connection if under limit
	p.mu.Lock()
	if p.created < p.cfg.PoolSize {
		p.created++
		p.mu.Unlock()
		return p.createConn()
	}
	p.mu.Unlock()

	// Wait for connection with context
	select {
	case conn := <-p.conns:
		if conn.IsClosed() {
			return p.createConn()
		}
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// createConn creates a new connection
func (p *Pool) createConn() (*Conn, error) {
	return newConn(p.cfg)
}

// Put returns a connection to the pool
func (p *Pool) Put(conn *Conn) {
	if conn == nil || conn.IsClosed() {
		return
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		conn.Close()
		return
	}
	p.mu.Unlock()

	select {
	case p.conns <- conn:
		// Returned to pool
	default:
		// Pool is full, close connection
		conn.Close()
		p.mu.Lock()
		p.created--
		p.mu.Unlock()
	}
}

// Close closes all connections in the pool
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	close(p.conns)
	for conn := range p.conns {
		conn.Close()
	}
	return nil
}

// Client is a high-level Redis client with connection pooling
type Client struct {
	pool *Pool
}

// NewClient creates a new Redis client
func NewClient(cfg Config) (*Client, error) {
	pool, err := NewPool(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{pool: pool}, nil
}

// Do executes a Redis command
func (c *Client) Do(ctx context.Context, args ...string) (Value, error) {
	conn, err := c.pool.Get(ctx)
	if err != nil {
		return Value{}, err
	}
	defer c.pool.Put(conn)

	return conn.Do(args...)
}

// Close closes the client and all connections
func (c *Client) Close() error {
	return c.pool.Close()
}

// Ping tests the connection
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.Do(ctx, "PING")
	if err != nil {
		return err
	}
	if resp.IsError {
		return fmt.Errorf("PING failed: %s", resp.Str)
	}
	return nil
}

// Config returns the configuration used for this client
func (c *Client) Config() Config {
	return c.pool.cfg
}

// PubSubMessage represents a message from Redis Pub/Sub
type PubSubMessage struct {
	Type    string // "subscribe", "psubscribe", "message", "pmessage", "unsubscribe", "punsubscribe"
	Pattern string // For pmessage/punsubscribe
	Channel string
	Data    string
}

// ReadPubSubMessage reads the next Pub/Sub message from a dedicated subscription connection.
// This should only be called on a connection that has already subscribed.
// Note: This uses a dedicated connection from the pool.
func (c *Client) ReadPubSubMessage(ctx context.Context) (*PubSubMessage, error) {
	conn, err := c.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	// Don't return connection to pool - it's used for subscription

	// Read the next array response
	resp, err := conn.reader.ReadValue()
	if err != nil {
		conn.Close()
		return nil, err
	}

	arr, err := resp.AsArray()
	if err != nil {
		return nil, err
	}

	if len(arr) < 3 {
		return nil, fmt.Errorf("invalid pub/sub message format")
	}

	msgType, _ := arr[0].AsString()

	msg := &PubSubMessage{
		Type: msgType,
	}

	switch msgType {
	case "message":
		msg.Channel, _ = arr[1].AsString()
		msg.Data, _ = arr[2].AsString()
	case "pmessage":
		if len(arr) >= 4 {
			msg.Pattern, _ = arr[1].AsString()
			msg.Channel, _ = arr[2].AsString()
			msg.Data, _ = arr[3].AsString()
		}
	case "subscribe", "psubscribe", "unsubscribe", "punsubscribe":
		msg.Channel, _ = arr[1].AsString()
	}

	return msg, nil
}

// GetDedicatedConn gets a connection for dedicated use (like Pub/Sub)
// The caller is responsible for closing this connection.
func (c *Client) GetDedicatedConn(ctx context.Context) (*Conn, error) {
	return c.pool.Get(ctx)
}
