# MQTT-to-IRC Bridge Bot - Implementation Plan

## Context

Building an IRC bot in Go that subscribes to MQTT topics and forwards messages to IRC channels. This enables integration between IoT/messaging systems (MQTT) and IRC chat rooms, useful for monitoring, alerts, and notifications. The project starts from an empty directory.

## Architecture Overview

### Component Structure
```
Main Application
    ├─> MQTT Client (goroutine) ──> Message Queue (channel)
    ├─> Bridge Worker (goroutine) ──> IRC Client (goroutine)
    └─> Health Server (goroutine)
```

### Technology Choices

**Libraries:**
- **MQTT**: `github.com/eclipse/paho.mqtt.golang` - Industry standard, robust reconnection, well-maintained
- **IRC**: `github.com/lrstanley/girc` - Modern, clean API, IRCv3 support, auto-reconnection
- **Config**: `github.com/spf13/viper` - Multi-format support (YAML/TOML/env), standard in Go ecosystem
- **Logging**: `github.com/rs/zerolog` - Zero-allocation structured logging, production-ready

### Message Flow
```
MQTT Topic → MQTT Handler → Message Queue → Bridge Mapper → IRC Formatter → IRC Client → IRC Channel
```

## Project Structure

```
mqtt2irc/
├── cmd/mqtt2irc/main.go           # Entry point, signal handling, shutdown coordination
├── internal/
│   ├── config/
│   │   ├── config.go              # Config structures, Viper loading, validation
│   │   └── validation.go          # Config validation logic
│   ├── mqtt/
│   │   ├── client.go              # MQTT wrapper, subscriptions, reconnection
│   │   └── handler.go             # Message handling
│   ├── irc/
│   │   ├── client.go              # IRC wrapper, channel joins, rate limiting
│   │   └── formatter.go           # Message formatting for IRC
│   ├── bridge/
│   │   ├── bridge.go              # Core orchestration, message queue processing
│   │   └── mapper.go              # Topic-to-channel mapping logic
│   └── health/
│       └── checker.go             # HTTP health endpoint
├── pkg/types/
│   └── message.go                 # Shared message types
├── configs/
│   └── config.example.yaml        # Example configuration
├── go.mod
├── go.sum
├── README.md
└── .gitignore
```

## Implementation Phases

### Phase 1: Core Functionality (MVP)
**Goal:** Basic working bridge

1. **Initialize project**
   - Create `go.mod` with dependencies
   - Set up project directory structure
   - Create `.gitignore`

2. **Configuration system**
   - Define config structures with validation tags
   - Implement Viper loading from YAML
   - Add config validation on startup
   - Create example config file

3. **MQTT client wrapper**
   - Wrap paho.mqtt.golang client
   - Implement connection handling with TLS support
   - Subscribe to topics from config
   - Forward messages to bridge via channel

4. **IRC client wrapper**
   - Wrap girc client
   - Implement connection with TLS support
   - Auto-join channels from config
   - Implement message sending with basic rate limiting

5. **Bridge core**
   - Create message queue (buffered channel)
   - Implement 1:1 topic-to-channel mapping
   - Process messages from MQTT to IRC
   - Basic message formatting

6. **Application lifecycle**
   - Main function with signal handling (SIGTERM/SIGINT)
   - Context-based shutdown coordination
   - Graceful shutdown with timeout
   - WaitGroups for goroutine coordination

7. **Basic logging**
   - Structured logging with zerolog
   - Log connections, disconnections, messages
   - Error logging

8. **Documentation**
   - README with setup instructions
   - Configuration documentation
   - Quick start guide

### Phase 2: Enhanced Features (Production-Ready)

1. **Advanced topic mapping**
   - MQTT wildcard support (`+` single level, `#` multi-level)
   - Multiple IRC channels per topic
   - Template-based message formatting

2. **Reliability improvements**
   - Exponential backoff reconnection (both clients)
   - Message queue with configurable size
   - Handle queue overflow (drop vs block)
   - Connection state monitoring

3. **Configuration enhancements**
   - Support TOML format
   - Environment variable overrides
   - Better validation error messages
   - Multiple mapping configurations

4. **IRC rate limiting**
   - Token bucket algorithm
   - Configurable messages/second
   - Prevent flood kicks

5. **Message handling**
   - Truncate long messages (IRC 512 byte limit)
   - Handle binary/non-UTF8 data
   - Message sanitization

### Phase 3: Monitoring & Operations

1. **Health checks**
   - HTTP endpoint (`/health`)
   - Report MQTT/IRC connection status
   - Ready for Kubernetes probes

2. **Container deployment**
   - Dockerfile
   - docker-compose.yml with test MQTT broker and IRC server
   - Example deployment files

3. **Testing**
   - Unit tests for mapper, formatter, validator
   - Integration tests with dockerized MQTT/IRC
   - CI/CD setup (GitHub Actions)

## Configuration Design

### Example YAML (`configs/config.example.yaml`)

```yaml
mqtt:
  broker: "tcp://mqtt.example.com:1883"
  client_id: "mqtt2irc_bot"
  username: "mqtt_user"
  password: "mqtt_password"
  qos: 1
  topics:
    - pattern: "sensors/temperature/#"
      qos: 1
    - pattern: "alerts/critical"
      qos: 2

irc:
  server: "irc.libera.chat:6697"
  use_tls: true
  nickname: "mqtt2irc"
  username: "mqtt2irc"
  realname: "MQTT to IRC Bridge"
  nickserv_password: ""
  rate_limit:
    messages_per_second: 2
    burst: 5

bridge:
  mappings:
    - mqtt_topic: "sensors/temperature/#"
      irc_channels:
        - "#iot-sensors"
      message_format: "[{{.Topic}}] {{.Payload}}"

    - mqtt_topic: "alerts/critical"
      irc_channels:
        - "#alerts"
        - "#ops"
      message_format: "🚨 ALERT: {{.Payload}}"

  queue:
    max_size: 1000
    block_on_full: false

  max_message_length: 400
  truncate_suffix: "..."

logging:
  level: "info"
  format: "json"

health:
  enabled: true
  port: 8080
```

### Environment Variables

Support override pattern:
```bash
MQTT2IRC_MQTT_BROKER=tcp://localhost:1883
MQTT2IRC_MQTT_USERNAME=user
MQTT2IRC_IRC_SERVER=irc.example.com:6667
MQTT2IRC_IRC_NICKNAME=mybot
```

## Key Implementation Details

### Graceful Shutdown Pattern

```go
// main.go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

bridge := bridge.New(cfg)

var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    bridge.Run(ctx)
}()

<-ctx.Done()

shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
bridge.Shutdown(shutdownCtx)

wg.Wait()
```

### Configuration Structures

```go
type Config struct {
    MQTT   MQTTConfig   `mapstructure:"mqtt"`
    IRC    IRCConfig    `mapstructure:"irc"`
    Bridge BridgeConfig `mapstructure:"bridge"`
    Logging LogConfig   `mapstructure:"logging"`
    Health HealthConfig `mapstructure:"health"`
}

type MQTTConfig struct {
    Broker   string        `mapstructure:"broker" validate:"required,url"`
    ClientID string        `mapstructure:"client_id" validate:"required"`
    Username string        `mapstructure:"username"`
    Password string        `mapstructure:"password"`
    QoS      byte          `mapstructure:"qos" validate:"min=0,max=2"`
    Topics   []TopicConfig `mapstructure:"topics" validate:"required,min=1"`
}

type MappingConfig struct {
    MQTTTopic     string   `mapstructure:"mqtt_topic" validate:"required"`
    IRCChannels   []string `mapstructure:"irc_channels" validate:"required,min=1"`
    MessageFormat string   `mapstructure:"message_format"`
}
```

### Message Type

```go
type Message struct {
    Topic     string
    Payload   []byte
    Timestamp time.Time
    QoS       byte
}
```

## Critical Files for Implementation

**Phase 1 Critical Path:**
1. `go.mod` - Module initialization with dependencies
2. `internal/config/config.go` - Config structures and loading
3. `cmd/mqtt2irc/main.go` - Application entry point and lifecycle
4. `internal/bridge/bridge.go` - Core orchestration logic
5. `internal/mqtt/client.go` - MQTT client wrapper
6. `internal/irc/client.go` - IRC client wrapper
7. `configs/config.example.yaml` - Example configuration

## Dependencies (go.mod)

```go
module github.com/yourusername/mqtt2irc

go 1.21

require (
    github.com/eclipse/paho.mqtt.golang v1.4.3
    github.com/lrstanley/girc v0.0.0-20230911145616-f11b3c8d2aa6
    github.com/spf13/viper v1.18.2
    github.com/rs/zerolog v1.32.0
)
```

## Security Considerations

1. **TLS by default** - Both MQTT and IRC should prefer TLS
2. **No hardcoded credentials** - Use env vars or secure config files
3. **Input validation** - Sanitize MQTT payloads before IRC (prevent injection)
4. **Rate limiting** - Prevent IRC flood kicks
5. **Message length limits** - Respect IRC 512-byte limit

## Testing Strategy

### Unit Tests
- Config parsing and validation (`internal/config/config_test.go`)
- Topic-to-channel mapping (`internal/bridge/mapper_test.go`)
- Message formatting (`internal/irc/formatter_test.go`)
- Wildcard matching (`internal/bridge/matcher_test.go`)

### Integration Tests
- End-to-end with Docker containers (mosquitto + ngircd)
- Reconnection scenarios
- Graceful shutdown
- Message delivery under load

### Manual Testing
```bash
# Test MQTT broker
docker run -d -p 1883:1883 eclipse-mosquitto

# Test IRC server
docker run -d -p 6667:6667 ngircd/ngircd

# Publish test messages
mosquitto_pub -t "sensors/temp" -m "25.5C"

# Connect IRC client
irssi -c localhost -p 6667
```

## Verification Plan

After implementation:

1. **Unit tests pass**: `go test ./...`
2. **Configuration loads**: Bot starts with example config
3. **MQTT connects**: Successful connection to broker, subscriptions confirmed
4. **IRC connects**: Successful connection, joins channels, NickServ auth works
5. **Message flow**: MQTT messages appear in IRC channels
6. **Mapping works**: Wildcard topics route to correct channels
7. **Formatting works**: Messages formatted per template
8. **Reconnection works**: Bot recovers from MQTT/IRC disconnections
9. **Graceful shutdown**: SIGTERM causes clean shutdown within timeout
10. **Health check works**: HTTP endpoint returns 200 when healthy

## Future Enhancements (Post-MVP)

- Bidirectional bridge (IRC → MQTT)
- Prometheus metrics
- Message filtering/transformation with regex
- Multiple MQTT brokers
- Dynamic subscription via IRC commands
- Hot config reload
- Message persistence during downtime
- Kubernetes deployment manifests
