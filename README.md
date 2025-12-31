# StreamRelay

A high-performance, real-time message relay package for Go, inspired by Centrifugo but built **entirely from scratch** with **zero external dependencies**.

## Features

- 🚀 **Zero Dependencies**: Only uses Go standard library
- 📡 **Custom RESP Protocol**: Direct TCP communication with Redis
- 🔥 **Redis Streams**: Full implementation of XADD, XREAD, XRANGE, XGROUP, etc.
- 🏠 **Room Management**: Groups and Direct Messages support
- 📦 **Offline Recovery**: Automatic message buffering and recovery
- 👥 **Presence Tracking**: Know who's online in each room
- 🔌 **Transport Agnostic**: Works with WebSocket, SSE, gRPC, or any transport
- ⚡ **High Performance**: Connection pooling, goroutine-per-room architecture
- 📋 **Client Protocol**: Centrifugo-like JSON protocol (connect, subscribe, publish, ping/pong)
- 🔒 **Permissions**: Role-based access control with channel patterns
- 📂 **Namespaces**: Group channels with shared configuration

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        StreamRelay                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                      Engine Layer                           │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │ │
│  │  │   Protocol   │  │  Permissions │  │    Namespaces    │  │ │
│  │  │ (Centrifugo) │  │    (RBAC)    │  │ (Channel Config) │  │ │
│  │  └──────────────┘  └──────────────┘  └──────────────────┘  │ │
│  │                                                             │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │ │
│  │  │    Relay     │  │    Rooms     │  │  ClientHandler   │  │ │
│  │  │  (Broker)    │  │  (Presence)  │  │ (Session Mgmt)   │  │ │
│  │  └──────────────┘  └──────────────┘  └──────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              │                                   │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                      Redis Layer                            │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │ │
│  │  │ RESP Parser  │  │  Conn Pool   │  │  Stream Commands │  │ │
│  │  │ (Protocol)   │  │   (TCP)      │  │ (XADD/XREAD...)  │  │ │
│  │  └──────────────┘  └──────────────┘  └──────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              │                                   │
└──────────────────────────────│───────────────────────────────────┘
                               ▼
                    ┌─────────────────┐
                    │   Redis Server  │
                    │    (via TCP)    │
                    └─────────────────┘
```

## Package Structure

```
streamrelay/
├── go.mod              # Zero external dependencies!
├── streamrelay.go      # Main package (re-exports)
├── README.md
│
├── redis/              # Custom RESP protocol & Redis client
│   ├── resp.go         # RESP protocol implementation
│   ├── client.go       # Connection & pooling
│   └── stream.go       # Redis Streams commands
│
└── engine/             # Centrifugo-like messaging engine
    ├── types.go        # Message, Subscriber, Events
    ├── room.go         # Room & presence management
    ├── relay.go        # Main orchestrator
    ├── options.go      # Functional options
    ├── protocol.go     # Client-server protocol (Centrifugo-like)
    ├── permissions.go  # RBAC & channel rules
    ├── namespace.go    # Namespace management
    └── handler.go      # Client session handler
```

## Installation

```bash
go get github.com/pico/streamrelay
```

## Quick Start

### 1. Create a Relay with Permissions and Namespaces

```go
package main

import (
    "context"
    "log"

    "github.com/pico/streamrelay"
)

func main() {
    // Create relay
    relay, err := streamrelay.NewWithOptions(
        streamrelay.WithRedisAddr("localhost:6379"),
        streamrelay.WithPrefix("app:"),
        streamrelay.WithEventHandler(func(e *streamrelay.Event) {
            log.Printf("[%s] room=%s user=%s", e.Type, e.RoomID, e.UserID)
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer relay.Close()

    // Setup permission checker
    permChecker := streamrelay.NewPermissionChecker()
    
    // Public channels: anyone can subscribe, only admins can publish
    permChecker.AddRule(streamrelay.ChannelRule{
        Pattern:            "public:*",
        DefaultPermissions: streamrelay.PermReadOnly,
        AllowAnonymous:     true,
    })
    
    // Chat channels: authenticated users can subscribe and publish
    permChecker.AddRule(streamrelay.ChannelRule{
        Pattern:            "chat:*",
        DefaultPermissions: streamrelay.PermStandard,
        RequireAuth:        true,
        JoinLeave:          true,
        Presence:           true,
    })
    
    // Admin role can publish everywhere
    permChecker.SetRolePermission("admin", streamrelay.PermAll)
    permChecker.AssignRole("user_admin", "admin")

    // Setup namespaces
    nsMgr := streamrelay.NewNamespaceManager()
    nsMgr.Register(streamrelay.ChatNamespace("chat:"))
    nsMgr.Register(streamrelay.NotificationNamespace("notify:"))
    nsMgr.Register(streamrelay.PublicNamespace("public:"))

    log.Println("StreamRelay ready!")
}
```

### 2. Handle WebSocket Connections

```go
// WebSocketTransport implements streamrelay.Transport
type WebSocketTransport struct {
    conn *websocket.Conn
    mu   sync.Mutex
}

func (t *WebSocketTransport) Write(data []byte) error {
    t.mu.Lock()
    defer t.mu.Unlock()
    return t.conn.WriteMessage(websocket.TextMessage, data)
}

func (t *WebSocketTransport) Close() error {
    return t.conn.Close()
}

// Handle new WebSocket connection
func handleWebSocket(w http.ResponseWriter, r *http.Request, relay *streamrelay.Relay) {
    conn, _ := upgrader.Upgrade(w, r, nil)
    
    transport := &WebSocketTransport{conn: conn}
    
    handler := streamrelay.NewClientHandler(streamrelay.ClientHandlerConfig{
        Transport:         transport,
        Relay:             relay,
        PermissionChecker: permChecker,  // From setup
        NamespaceManager:  nsMgr,        // From setup
        PingInterval:      25 * time.Second,
    })
    
    // Read messages and pass to handler
    for {
        _, data, err := conn.ReadMessage()
        if err != nil {
            break
        }
        handler.HandleMessage(data)
    }
    
    handler.Close()
}
```

### 3. Client Protocol (JavaScript Example)

```javascript
// Connect to StreamRelay via WebSocket
const ws = new WebSocket('ws://localhost:8080/ws');

let cmdId = 0;

// Connect command
ws.onopen = () => {
    ws.send(JSON.stringify({
        id: ++cmdId,
        type: 'connect',
        data: { token: 'user_alice' }
    }));
};

// Subscribe to a channel
function subscribe(channel) {
    ws.send(JSON.stringify({
        id: ++cmdId,
        type: 'subscribe',
        channel: channel,
        data: { recover: true }
    }));
}

// Publish a message
function publish(channel, message) {
    ws.send(JSON.stringify({
        id: ++cmdId,
        type: 'publish',
        channel: channel,
        data: { data: message }
    }));
}

// Handle incoming messages
ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    
    if (msg.reply) {
        // Response to our command
        console.log('Reply:', msg.reply);
    } else if (msg.push) {
        // Server push
        switch (msg.push.type) {
            case 'message':
                console.log('New message:', msg.push.data);
                break;
            case 'join':
                console.log('User joined:', msg.push.data);
                break;
            case 'leave':
                console.log('User left:', msg.push.data);
                break;
        }
    }
};
```

## Client Protocol Reference

### Commands (Client → Server)

| Command | Description | Fields |
|---------|-------------|--------|
| `connect` | Establish connection | `token`, `data` |
| `subscribe` | Subscribe to channel | `channel`, `token`, `recover`, `offset` |
| `unsubscribe` | Unsubscribe from channel | `channel` |
| `publish` | Publish message | `channel`, `data` |
| `presence` | Get online users | `channel` |
| `history` | Get message history | `channel`, `limit`, `since` |
| `ping` | Keepalive | - |

### Pushes (Server → Client)

| Push | Description | Data |
|------|-------------|------|
| `message` | New message | `offset`, `data`, `info` |
| `join` | User joined channel | `info` |
| `leave` | User left channel | `info` |
| `pong` | Ping response | - |
| `disconnect` | Forced disconnect | `code`, `reason`, `reconnect` |

## Permissions

### Permission Levels

```go
PermNone      // No access
PermSubscribe // Can subscribe to channel
PermPublish   // Can publish to channel
PermPresence  // Can see who's online
PermHistory   // Can access message history
PermJoinLeave // Receives join/leave events

PermReadOnly  // Subscribe + Presence + History
PermStandard  // Subscribe + Publish + Presence
PermAll       // All permissions
```

### Channel Rules

```go
permChecker.AddRule(streamrelay.ChannelRule{
    Pattern:            "private:*",      // Pattern with * wildcard
    DefaultPermissions: streamrelay.PermNone,
    RequireAuth:        true,
    MaxSubscribers:     100,
    JoinLeave:          true,
    Presence:           true,
})
```

### Role-Based Access

```go
// Define roles
permChecker.SetRolePermission("moderator", streamrelay.PermAll)
permChecker.SetRolePermission("vip", streamrelay.PermStandard)

// Assign roles to users
permChecker.AssignRole("user_123", "moderator")
permChecker.AssignRole("user_456", "vip")

// Per-user overrides
permChecker.SetUserPermission("user_789", "chat:general", streamrelay.PermReadOnly)
```

## Namespaces

Namespaces group channels with shared configuration:

```go
nsMgr := streamrelay.NewNamespaceManager()

// Built-in presets
nsMgr.Register(streamrelay.ChatNamespace("chat:"))         // Chat rooms
nsMgr.Register(streamrelay.NotificationNamespace("notify:")) // Notifications
nsMgr.Register(streamrelay.PresenceNamespace("presence:"))   // Presence-only
nsMgr.Register(streamrelay.PublicNamespace("public:"))       // Public/anonymous
nsMgr.Register(streamrelay.PersonalNamespace("user:"))       // Per-user channels

// Custom namespace
nsMgr.Register(&streamrelay.Namespace{
    Name:          "game",
    ChannelPrefix: "game:",
    Options: streamrelay.NamespaceOptions{
        HistorySize:    50,
        HistoryTTL:     time.Hour,
        Presence:       true,
        JoinLeave:      true,
        Publish:        true,
        MaxSubscribers: 8,  // Max 8 players per game
    },
})
```

## Stats & Monitoring

```go
stats := relay.Stats()
fmt.Printf("Rooms: %d\n", stats.ActiveRooms)
fmt.Printf("Subscribers: %d\n", stats.ActiveSubscribers)
fmt.Printf("Messages: %d\n", stats.TotalMessages)
fmt.Printf("Uptime: %ds\n", stats.Uptime)
```

## Comparison with Centrifugo

| Feature | Centrifugo | StreamRelay |
|---------|------------|-------------|
| Dependencies | Many | **Zero** |
| Redis Protocol | Uses go-redis | **Custom RESP** |
| Deployment | Standalone Server | **Library** |
| Client Protocol | JSON/Protobuf | **JSON** |
| Namespaces | ✅ | ✅ |
| Permissions | Limited | **Full RBAC** |
| History/Recovery | ✅ | ✅ |
| Presence | ✅ | ✅ |
| Join/Leave | ✅ | ✅ |
| Customization | Config-based | **Full Code Access** |

## License

MIT License
