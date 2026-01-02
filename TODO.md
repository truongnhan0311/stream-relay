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

## 🔥 Horizontal Scaling (Priority)

### Redis Pub/Sub Sync Between Nodes

When running multiple StreamRelay servers, messages need to sync between them.

**Current state**: Single server only - all users must connect to same server.

**Goal**: Multiple servers can handle the same rooms, messages sync via Redis Pub/Sub.

```
Server 1 (10K users)  ───┐
                         │
Server 2 (10K users)  ───┼───▶ Redis Pub/Sub ───▶ All servers receive
                         │
Server 3 (10K users)  ───┘
```

**Implementation needed**:

1. [ ] Add `Broker` interface for inter-node communication
2. [ ] Implement `RedisBroker` using Redis PUBLISH/SUBSCRIBE
3. [ ] When `Relay.Publish()` is called:
   - Write to Redis Stream (existing - for persistence)
   - PUBLISH to Redis Pub/Sub channel (new - for sync)
4. [ ] Each Relay instance subscribes to pattern `sr:pub:*`
5. [ ] On receiving pub/sub message, broadcast to local subscribers only
6. [ ] Add node ID to avoid echo (don't re-broadcast own messages)

**Proposed API**:

```go
type Broker interface {
    Publish(ctx context.Context, channel string, data []byte) error
    Subscribe(ctx context.Context, pattern string) (<-chan BrokerMessage, error)
}

type RedisBroker struct {
    client *redis.Client
    nodeID string // To filter own messages
}

// Usage:
relay, _ := streamrelay.NewWithOptions(
    streamrelay.WithRedisAddr("localhost:6379"),
    streamrelay.WithBroker(redisBroker),  // Enable multi-node sync
)
```

**Pub/Sub message format**:
```json
{
    "node": "node-1",
    "room": "chat:general", 
    "data": {...message...}
}
```

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
