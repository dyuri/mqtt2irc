# mqtt2irc - MQTT to IRC Bridge Bot

A lightweight, reliable bridge that forwards messages from MQTT topics to IRC channels. Perfect for monitoring IoT devices, receiving alerts, or integrating MQTT-based systems with IRC chat rooms.

## Features

- **Flexible Topic Mapping**: Map MQTT topics to IRC channels with wildcard support (`+` and `#`)
- **Multiple Targets**: Forward a single MQTT topic to multiple IRC channels
- **Message Formatting**: Customizable message templates using Go templates
- **JSON Payload Support**: Automatically parses JSON payloads — access individual fields with `{{.JSON.fieldname}}`
- **Binary Payload Safety**: Binary (non-UTF-8) payloads are displayed as `[binary data, N bytes]` instead of garbled output
- **Message Processors**: Per-mapping pre-processors for deduplication, type-based routing, and custom formatting (built-in: Meshtastic)
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
- `{{.Payload}}` - Message payload as string (binary payloads shown as `[binary data, N bytes]`)
- `{{.QoS}}` - MQTT QoS level (0, 1, or 2)
- `{{.JSON.fieldname}}` - Individual field from a JSON object payload (empty string if field missing or payload is not JSON)

Examples:
```yaml
# Plain text payload
message_format: "[{{.Topic}}] {{.Payload}}"

# JSON payload — access individual fields
message_format: "{{.Topic}}: temp={{.JSON.temperature}} humidity={{.JSON.humidity}}"

# Mix of fields
message_format: "[{{.JSON.device}}] {{.JSON.status}} ({{.Topic}})"
```

**JSON field notes:**
- `{{.JSON}}` is populated only when the payload is a valid JSON **object** (`{...}`). Arrays and scalar values are not parsed — use `{{.Payload}}` for those.
- All field values are stringified, so numbers and booleans work directly in templates.
- Missing fields produce an empty string (no error, no `<no value>` text).
- `{{.Payload}}` always contains the raw payload string regardless of whether JSON parsing succeeded.

### Message Processors

Processors are optional per-mapping hooks that run before the normal template formatting. A processor can filter (drop) a message or provide its own pre-formatted output.

```yaml
bridge:
  mappings:
    - mqtt_topic: "some/topic/#"
      irc_channels:
        - "#mychannel"
      processor: "meshtastic"       # processor name
      processor_config:             # processor-specific settings
        dedup_window: "30s"
```

When a processor is set, `message_format` is only used as a fallback if the processor passes the message through without a formatted result.

#### Built-in: `meshtastic`

Designed for [Meshtastic](https://meshtastic.org/) mesh radio networks. Handles the heterogeneous JSON message types that Meshtastic nodes publish over MQTT.

**What it does:**
- **Deduplication**: Drops messages whose `id` field was seen within `dedup_window` (prevents duplicates caused by mesh re-broadcasts)
- **Type routing**: Selects a format template based on the `type` field of the JSON payload
- **Payload flattening**: Hoists fields from the nested `payload` sub-object to the top level for easy template access
- **Integer rendering**: JSON numbers are rendered as plain integers (e.g. `479000000`, not `4.79e+08`)

**`processor_config` options:**

| Key | Default | Description |
|-----|---------|-------------|
| `dedup_window` | `30s` | Drop duplicate message IDs within this duration |
| `id_field` | `id` | JSON field used for deduplication |
| `type_field` | `type` | JSON field that selects the format template |
| `formats` | see below | Map of message type → Go template string |

**Default format templates:**

```yaml
formats:
  nodeinfo:  "Node {{.from}} - {{.longName}} ({{.hwModel}})"
  position:  "{{.from}} @ {{.latitudeI}},{{.longitudeI}} alt={{.altitude}}m"
  text:      "{{.from}}: {{.text}}"
  telemetry: "{{.from}} bat={{.batteryLevel}}%"
  default:   "[{{.msgtype}}] from {{.from}}"
```

Override any subset of formats in `processor_config.formats`. The `default` template is used when the message type doesn't match any other key. All top-level and `payload` sub-object fields are available as template variables (e.g. `{{.from}}`, `{{.text}}`, `{{.longName}}`). The `type` field is available as `{{.msgtype}}` to avoid collision with Go template internals.

**Full Meshtastic example:**

```yaml
mqtt:
  topics:
    - pattern: "msh/EU_868/HU/#"
      qos: 1

bridge:
  mappings:
    - mqtt_topic: "msh/EU_868/HU/#"
      irc_channels:
        - "#meshtastic"
      processor: "meshtastic"
      processor_config:
        dedup_window: "30s"
        formats:
          nodeinfo:  "Node {{.from}} - {{.longName}} ({{.hwModel}})"
          position:  "{{.from}} @ {{.latitudeI}},{{.longitudeI}} alt={{.altitude}}m"
          text:      "{{.from}}: {{.text}}"
          telemetry: "{{.from}} bat={{.batteryLevel}}%"
          default:   "[{{.msgtype}}] from {{.from}}"
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

Build and run with a host-mounted config file:

```bash
docker build -t mqtt2irc .

# Mount a single config file
docker run -v /path/to/config.yaml:/etc/mqtt2irc/config.yaml mqtt2irc

# Or mount the entire config directory
docker run -v /path/to/configs:/etc/mqtt2irc mqtt2irc
```

With docker-compose:

```yaml
services:
  mqtt2irc:
    image: mqtt2irc
    volumes:
      - ./configs/config.yaml:/etc/mqtt2irc/config.yaml:ro
    restart: unless-stopped
```

The container reads config from `/etc/mqtt2irc/config.yaml` by default. The directory is declared as a `VOLUME` so Docker treats it as a mount point, and it is owned by the non-root `mqtt2irc` user inside the container.

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

### Meshtastic Mesh Network

Bridge a Meshtastic MQTT feed to IRC with deduplication and type-aware formatting:

```yaml
mqtt:
  topics:
    - pattern: "msh/EU_868/HU/#"
      qos: 1

bridge:
  mappings:
    - mqtt_topic: "msh/EU_868/HU/#"
      irc_channels: ["#meshtastic"]
      processor: "meshtastic"
      processor_config:
        dedup_window: "30s"
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
│   │   └── processors/    # Built-in message processors (meshtastic, ...)
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
- Check MQTT wildcards (`+`, `#`) are used correctly — the subscription pattern in `mqtt.topics` must also use wildcards if you want subtopics (e.g. `msh/EU_868/HU/#`, not just `msh/EU_868/HU`)
- Enable debug logging: `logging.level: "debug"`
- Ensure the bot has joined the target channels

### JSON fields not showing in formatted messages

- Confirm the payload is a JSON **object** (`{...}`); arrays and scalar values are not parsed
- Use `{{.Payload}}` to inspect the raw payload and verify it is valid JSON
- Field names are case-sensitive and must match exactly
- If a field is missing, the template produces an empty string — check for typos in field names

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
