# mqtt2irc - MQTT to IRC Bridge Bot

A lightweight, reliable bridge that forwards messages from MQTT topics to IRC channels. Perfect for monitoring IoT devices, receiving alerts, or integrating MQTT-based systems with IRC chat rooms.

## Features

- **Flexible Topic Mapping**: Map MQTT topics to IRC channels with wildcard support (`+` and `#`)
- **Multiple Targets**: Forward a single MQTT topic to multiple IRC channels
- **Message Formatting**: Customizable message templates using Go templates
- **Rate Limiting**: Built-in token bucket rate limiter to prevent IRC flood kicks
- **Auto-Reconnection**: Automatic reconnection to both MQTT and IRC with exponential backoff
- **TLS Support**: Secure connections for both MQTT and IRC
- **Health Checks**: HTTP endpoints for monitoring and Kubernetes probes
- **Graceful Shutdown**: Clean shutdown handling with configurable timeout
- **Structured Logging**: JSON or console logging with configurable levels

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/dyuri/mqtt2irc.git
cd mqtt2irc

# Build the binary
go build -o mqtt2irc ./cmd/mqtt2irc

# Or install directly
go install ./cmd/mqtt2irc
```

### Configuration

Copy the example configuration and edit it for your setup:

```bash
cp configs/config.example.yaml configs/config.yaml
vim configs/config.yaml
```

Minimal configuration example:

```yaml
mqtt:
  broker: "tcp://localhost:1883"
  client_id: "mqtt2irc_bot"
  topics:
    - pattern: "sensors/#"
      qos: 1

irc:
  server: "irc.libera.chat:6697"
  use_tls: true
  nickname: "mybot"
  username: "mybot"
  realname: "MQTT Bridge Bot"

bridge:
  mappings:
    - mqtt_topic: "sensors/#"
      irc_channels:
        - "#mychannel"
      message_format: "[{{.Topic}}] {{.Payload}}"
```

### Running

```bash
# Run with config file
./mqtt2irc -config configs/config.yaml

# Or let it auto-detect config.yaml in current directory or ./configs
./mqtt2irc
```

### Environment Variables

Override configuration values using environment variables:

```bash
export MQTT2IRC_MQTT_BROKER="tcp://mqtt.example.com:1883"
export MQTT2IRC_MQTT_USERNAME="user"
export MQTT2IRC_IRC_SERVER="irc.example.com:6667"
export MQTT2IRC_IRC_NICKNAME="mybot"

./mqtt2irc
```

## Configuration Reference

### MQTT Configuration

```yaml
mqtt:
  broker: "tcp://mqtt.example.com:1883"  # MQTT broker URL
  client_id: "mqtt2irc_bot"              # Unique client identifier
  username: "user"                        # Optional username
  password: "pass"                        # Optional password
  use_tls: true                           # Enable TLS/SSL
  qos: 1                                  # Default QoS (0, 1, or 2)
  topics:                                 # Topics to subscribe to
    - pattern: "sensors/#"                # MQTT topic pattern
      qos: 1                              # QoS for this topic
```

**MQTT Wildcards:**
- `+` - Matches a single level (e.g., `sensors/+/temp` matches `sensors/bedroom/temp`)
- `#` - Matches multiple levels (e.g., `sensors/#` matches all under `sensors/`)

### IRC Configuration

```yaml
irc:
  server: "irc.libera.chat:6697"    # IRC server (host:port)
  use_tls: true                      # Enable TLS/SSL
  nickname: "mqtt2irc"               # Bot nickname
  username: "mqtt2irc"               # Bot username
  realname: "MQTT Bridge"            # Bot realname
  nickserv_password: ""              # NickServ password (optional)
  rate_limit:
    messages_per_second: 2           # Max messages per second
    burst: 5                         # Burst capacity
```

### Bridge Configuration

```yaml
bridge:
  mappings:
    - mqtt_topic: "sensors/temp/#"        # MQTT topic pattern
      irc_channels:                        # IRC channels to forward to
        - "#sensors"
        - "#monitoring"
      message_format: "[{{.Topic}}] {{.Payload}}"  # Go template

  queue:
    max_size: 1000                   # Message queue buffer size
    block_on_full: false             # Drop or block when full

  max_message_length: 400            # Max IRC message length
  truncate_suffix: "..."             # Suffix for truncated messages
```

**Message Format Templates:**

Templates use Go's `text/template` syntax with the following fields:
- `{{.Topic}}` - MQTT topic name
- `{{.Payload}}` - Message payload as string
- `{{.QoS}}` - MQTT QoS level (0, 1, or 2)

Examples:
```yaml
message_format: "[{{.Topic}}] {{.Payload}}"
message_format: "Sensor update: {{.Payload}}"
message_format: "{{.Payload}} (from {{.Topic}})"
```

### Logging Configuration

```yaml
logging:
  level: "info"      # trace, debug, info, warn, error, fatal, panic
  format: "console"  # json or console
```

### Health Check Configuration

```yaml
health:
  enabled: true  # Enable health check server
  port: 8080     # HTTP port for health endpoints
```

**Endpoints:**
- `GET /health` - Returns JSON with connection status and queue info
- `GET /ready` - Returns 200 if ready, 503 if not (Kubernetes readiness probe)

## Docker Deployment

Create a `Dockerfile`:

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o mqtt2irc ./cmd/mqtt2irc

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/mqtt2irc /usr/local/bin/
COPY configs/config.yaml /etc/mqtt2irc/config.yaml
CMD ["mqtt2irc", "-config", "/etc/mqtt2irc/config.yaml"]
```

Build and run:

```bash
docker build -t mqtt2irc .
docker run -v ./config.yaml:/etc/mqtt2irc/config.yaml mqtt2irc
```

## Example Use Cases

### IoT Sensor Monitoring

Monitor temperature sensors and post to IRC:

```yaml
mqtt:
  topics:
    - pattern: "home/+/temperature"
      qos: 1

bridge:
  mappings:
    - mqtt_topic: "home/+/temperature"
      irc_channels: ["#home-automation"]
      message_format: "🌡️ {{.Payload}}°C"
```

### Critical Alerts

Forward critical alerts to multiple channels:

```yaml
mqtt:
  topics:
    - pattern: "alerts/critical/#"
      qos: 2

bridge:
  mappings:
    - mqtt_topic: "alerts/critical/#"
      irc_channels: ["#alerts", "#ops", "#oncall"]
      message_format: "🚨 CRITICAL: {{.Payload}}"
```

### Multi-Environment Monitoring

Separate production and staging environments:

```yaml
bridge:
  mappings:
    - mqtt_topic: "production/logs/#"
      irc_channels: ["#prod-logs"]
      message_format: "[PROD] {{.Payload}}"

    - mqtt_topic: "staging/logs/#"
      irc_channels: ["#staging-logs"]
      message_format: "[STAGE] {{.Payload}}"
```

## Development

### Project Structure

```
mqtt2irc/
├── cmd/mqtt2irc/          # Application entry point
├── internal/
│   ├── bridge/            # Bridge orchestration and mapping
│   ├── config/            # Configuration loading and validation
│   ├── health/            # Health check HTTP server
│   ├── irc/               # IRC client wrapper
│   └── mqtt/              # MQTT client wrapper
├── pkg/types/             # Shared types
└── configs/               # Configuration examples
```

### Building

```bash
# Build for current platform
go build -o mqtt2irc ./cmd/mqtt2irc

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o mqtt2irc-linux ./cmd/mqtt2irc

# Build for macOS
GOOS=darwin GOARCH=amd64 go build -o mqtt2irc-macos ./cmd/mqtt2irc
```

### Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/bridge/
```

## Troubleshooting

### Bot doesn't connect to IRC

- Check TLS settings match your server (try `use_tls: false` for non-TLS servers)
- Verify server address includes port (e.g., `irc.libera.chat:6697`)
- Check firewall rules allow outbound connections

### Messages aren't appearing in IRC

- Verify topic mappings match your MQTT topics exactly
- Check MQTT wildcards (`+`, `#`) are used correctly
- Enable debug logging: `logging.level: "debug"`
- Ensure the bot has joined the target channels

### Bot gets kicked for flooding

- Reduce `rate_limit.messages_per_second`
- Increase `rate_limit.burst` for occasional bursts
- Check your MQTT topics aren't publishing too frequently

### MQTT connection drops frequently

- Enable TLS: `mqtt.use_tls: true`
- Verify broker supports the configured QoS levels
- Check network stability and broker logs

## License

MIT License - see LICENSE file for details

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## Support

For bugs and feature requests, please open an issue on GitHub.
