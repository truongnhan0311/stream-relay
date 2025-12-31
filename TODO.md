# StreamRelay - TODO

## Fan-In / Fan-Out Optimizations

### 1. Batch Delivery
- [ ] Group multiple messages before sending to reduce network overhead
- [ ] Add configurable batch size (e.g., 10 messages)
- [ ] Add configurable batch timeout (e.g., 50ms)
- [ ] Flush batch on timeout or when size limit reached

```go
// Proposed API:
type BatchConfig struct {
    MaxSize    int           // Max messages per batch
    MaxWait    time.Duration // Max time to wait before flushing
    Enabled    bool
}
```

### 2. Priority Queues
- [ ] Add message priority levels (High, Normal, Low)
- [ ] Deliver high-priority messages first
- [ ] System messages (join/leave) should be high priority
- [ ] Typing indicators should be low priority

```go
// Proposed API:
type Priority int
const (
    PriorityHigh   Priority = 1
    PriorityNormal Priority = 5
    PriorityLow    Priority = 10
)

type PublishOptions struct {
    Priority Priority
    // ...
}
```

### 3. Rate Limiting
- [ ] Prevent message flooding from single user
- [ ] Configurable rate limit per user per channel
- [ ] Configurable rate limit per channel (global)
- [ ] Return error when rate limit exceeded

```go
// Proposed API:
type RateLimitConfig struct {
    MessagesPerSecond int
    BurstSize         int
    Enabled           bool
}

type ChannelRule struct {
    MaxPublishRate int // messages per second per user
    // ...
}
```

---

## Other Future Improvements

### Redis
- [ ] Redis Cluster support
- [ ] Redis Sentinel support
- [ ] Connection retry with exponential backoff
- [ ] Pipeline commands for better performance

### Protocol
- [ ] Protobuf encoding option (in addition to JSON)
- [ ] WebSocket binary frames support
- [ ] Message compression (gzip/deflate)
- [ ] Server-to-server RPC

### Security
- [ ] JWT token validation (built-in)
- [ ] Channel token expiration
- [ ] Connection token refresh
- [ ] IP-based rate limiting

### Scalability
- [ ] Horizontal scaling with Redis pub/sub between nodes
- [ ] Sticky sessions support
- [ ] Consistent hashing for room distribution

### Monitoring
- [ ] Prometheus metrics export
- [ ] Health check endpoint
- [ ] Message latency tracking
- [ ] Per-channel statistics

### Features
- [ ] Message editing
- [ ] Message deletion
- [ ] Reactions (emoji)
- [ ] Read receipts
- [ ] Typing indicators optimization
- [ ] File/media attachments metadata

---

## Completed ✅

- [x] Custom RESP protocol implementation
- [x] Redis connection pooling
- [x] Redis Streams (XADD, XREAD, XRANGE, XGROUP, XACK)
- [x] Room management
- [x] Subscriber interface
- [x] Fan-in / Fan-out delivery
- [x] Offline message recovery
- [x] Presence tracking
- [x] Client protocol (connect, subscribe, publish, ping/pong)
- [x] Permissions system (RBAC)
- [x] Channel rules with pattern matching
- [x] Namespaces with shared config
- [x] ClientHandler for session management
- [x] Join/Leave notifications
