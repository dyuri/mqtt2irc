# CLAUDE.md - MQTT-to-IRC Bridge Project

This file contains project context, architecture decisions, and conventions for AI assistants working on this codebase.

## Project Overview

**mqtt2irc** is a production-ready Go application that bridges MQTT topics to IRC channels. It subscribes to MQTT broker topics and forwards messages to IRC channels with configurable mappings, formatting, and rate limiting.

**Use Cases:**
- IoT sensor monitoring in IRC
- Server alerts and notifications
- Real-time system status updates
- Multi-environment log aggregation

## Architecture

### Design Principles

1. **Concurrent by Design**: Separate goroutines for MQTT, IRC, bridge worker, and health server
2. **Context-Based Cancellation**: All components respond to context cancellation for graceful shutdown
3. **Channel-Based Message Queue**: Buffered channel between MQTT and bridge for decoupling
4. **Fail-Safe**: Auto-reconnection with exponential backoff, message dropping on overflow
5. **Production-Ready**: TLS support, health checks, structured logging, rate limiting

### Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         Main App                             │
│  • Signal handling (SIGTERM, SIGINT)                        │
│  • Graceful shutdown coordination (30s timeout)             │
│  • WaitGroup for goroutine management                       │
└─────────────────────────────────────────────────────────────┘
                              │
                ┌─────────────┼─────────────┐
                │             │             │
                ▼             ▼             ▼
         ┌──────────┐  ┌──────────┐  ┌──────────┐
         │   MQTT   │  │  Bridge  │  │  Health  │
         │  Client  │  │  Worker  │  │  Server  │
         └──────────┘  └──────────┘  └──────────┘
              │             │             │
              │             │             │
              ▼             ▼             ▼
         [Paho MQTT]   [Mapper]      [HTTP :8080]
              │         [Formatter]       │
              │             │             │
              ▼             ▼             │
        [msg channel] → [IRC Client] ←────┘
                             │
                             ▼
                          [girc]
```

### Message Flow

```
MQTT Broker → MQTT Client → messageHandler → Message Queue (chan)
    ↓
Bridge Worker reads from queue → Mapper (match topics)
    ↓
Processor (optional, per-mapping): filter / pre-format
    ↓ (pass-through or pre-formatted)
Formatter (apply template) → IRC Client (rate limit)
    ↓
IRC Server
```

### Key Libraries

| Library | Purpose | Why Chosen |
|---------|---------|------------|
| `github.com/eclipse/paho.mqtt.golang` | MQTT client | Industry standard, robust auto-reconnect, active maintenance |
| `github.com/lrstanley/girc` | IRC client | Modern, clean API, IRCv3 support, auto-reconnect |
| `github.com/spf13/viper` | Configuration | Multi-format support, env vars, widely used |
| `github.com/rs/zerolog` | Logging | Zero-allocation structured logging, fast |
| `golang.org/x/time/rate` | Rate limiting | Standard library quality, token bucket |

## Code Organization

```
mqtt2irc/
├── cmd/mqtt2irc/           # Application entry point only
│   └── main.go             # Signal handling, lifecycle, logger setup, admin wiring
├── internal/               # Private application code
│   ├── admin/              # IRC admin command handler
│   │   ├── handler.go      # BridgeAdmin interface, Config, Handler, auth, dispatch
│   │   └── commands.go     # Individual command implementations
│   ├── bridge/             # Core business logic
│   │   ├── bridge.go       # Orchestrates MQTT→IRC flow + admin delegate methods
│   │   ├── mapper.go       # Topic pattern matching (+ and # wildcards)
│   │   ├── processor.go    # Processor interface, ProcessResult, registry
│   │   └── processors/     # Built-in processor implementations
│   │       └── meshtastic.go  # Meshtastic JSON processor + init() registration
│   ├── config/             # Configuration management
│   │   ├── config.go       # Viper loading, structures, defaults
│   │   └── validation.go   # Config validation rules
│   ├── mqtt/               # MQTT client wrapper
│   │   └── client.go       # Wraps paho.mqtt, handles reconnection
│   ├── irc/                # IRC client wrapper
│   │   ├── client.go       # Wraps girc, rate limiting, channel joins, Nick/Reconnect
│   │   └── formatter.go    # Message templating, sanitization, truncation
│   └── health/             # Health check HTTP server
│       └── checker.go      # /health and /ready endpoints
└── pkg/types/              # Shared types (could be public)
    └── message.go          # Message struct
```

### Package Responsibilities

- **cmd/mqtt2irc**: Application bootstrap only. No business logic. Blank-imports `bridge/processors` to trigger processor registration. Wires admin handler if enabled.
- **internal/admin**: IRC PRIVMSG-based admin command handler. Defines `BridgeAdmin` interface (no import of `bridge` — avoids circular import). Wired in `main.go`.
- **internal/bridge**: Core orchestration. Owns message flow, mapping logic, and the processor registry. Exposes delegate methods that implement `admin.BridgeAdmin`.
- **internal/bridge/processors**: Built-in processor implementations. Each registers itself via `init()`.
- **internal/config**: All configuration concerns. Validation happens here.
- **internal/mqtt**: MQTT client abstraction. Hides paho.mqtt implementation.
- **internal/irc**: IRC client abstraction. Hides girc implementation.
- **internal/health**: HTTP health endpoints. Exposes bridge status.
- **pkg/types**: Shared data structures. Pure data, no behavior.

## Important Implementation Details

### MQTT Wildcards

The mapper implements MQTT wildcard matching:

- `+` matches **single level**: `sensors/+/temp` matches `sensors/bedroom/temp` but NOT `sensors/bedroom/bathroom/temp`
- `#` matches **multiple levels**: `sensors/#` matches `sensors/temp` AND `sensors/bedroom/temp/reading`
- `#` must be **last**: `sensors/#/temp` is INVALID

**Algorithm**: Recursive matching in `mapper.go:matchParts()`

### Rate Limiting

Uses **token bucket algorithm** from `golang.org/x/time/rate`:

```go
limiter := rate.NewLimiter(
    rate.Limit(messagesPerSecond),  // Refill rate
    burst,                           // Bucket capacity
)
```

Blocks until token available. Prevents IRC kicks for flooding.

### Message Formatting

Uses Go's `text/template` with these fields:

- `{{.Topic}}` - MQTT topic string
- `{{.Payload}}` - Message payload as string
- `{{.QoS}}` - MQTT QoS level (0, 1, 2)

**Sanitization**: Strips control characters, collapses spaces, preserves UTF-8
**Truncation**: Respects IRC 512-byte limit, rune-aware

### Graceful Shutdown

1. `signal.NotifyContext()` creates cancellable context
2. On SIGTERM/SIGINT, context is cancelled
3. All goroutines see `ctx.Done()` and exit
4. Bridge waits for message queue to drain
5. MQTT/IRC disconnect cleanly
6. 30-second timeout enforced

### Health Checks

- **GET /health**: Returns JSON with connection status
  - 200 if both MQTT and IRC connected
  - 503 if either disconnected
  - Includes queue size and capacity

- **GET /ready**: Kubernetes readiness probe
  - 200 "ready" if healthy
  - 503 "not ready" if unhealthy

## Configuration Conventions

### Structure Naming

- Use `mapstructure` tags for YAML field names (snake_case)
- Use PascalCase for Go struct fields
- Nested configs use separate structs (MQTTConfig, IRCConfig, etc.)

### Defaults

Set in `config.Load()` using `viper.SetDefault()`:

- MQTT QoS: 1
- IRC rate limit: 2 msg/sec, burst 5
- Queue size: 1000
- Max message length: 400 bytes
- Health port: 8080
- Logging: info level, JSON format

### Environment Variables

Pattern: `MQTT2IRC_<section>_<field>`

Examples:
- `MQTT2IRC_MQTT_BROKER`
- `MQTT2IRC_IRC_NICKNAME`
- `MQTT2IRC_LOGGING_LEVEL`

Configured via `viper.SetEnvPrefix()` and `viper.AutomaticEnv()`

## Testing Strategy

### Unit Tests

**What to test:**
- Pure functions (mapper, formatter)
- Pattern matching logic
- Validation rules
- Template rendering

**What NOT to test:**
- Network I/O (MQTT/IRC connections)
- Goroutine coordination (integration tests for this)

### Test Organization

- Test files alongside implementation: `mapper_test.go` next to `mapper.go`
- Table-driven tests for multiple cases
- Descriptive test names: `TestMatchTopic/+_match`

### Running Tests

```bash
make test          # Run all tests
make test-cover    # Generate coverage report
go test ./internal/bridge -v  # Test specific package
```

## Common Tasks

### Adding a New Configuration Field

1. Add field to appropriate struct in `internal/config/config.go`
2. Add `mapstructure` tag
3. Add default in `config.Load()` if needed
4. Add validation in `internal/config/validation.go`
5. Update `configs/config.example.yaml`
6. Document in README.md

### Adding a New Mapping Feature

1. Modify `MappingConfig` struct if config change needed
2. Update `mapper.go` matching logic
3. Add tests in `mapper_test.go`
4. Update example configs
5. Document in README.md

### Adding a New Processor

1. Create `internal/bridge/processors/<name>.go` in package `processors`
2. Implement `bridge.Processor` interface: `Process(msg types.Message) (bridge.ProcessResult, error)`
3. Add a constructor `func newXxxProcessor(config map[string]interface{}) (bridge.Processor, error)`
4. Register in `init()`: `bridge.Register("name", newXxxProcessor)`
5. Add tests in `<name>_test.go`
6. Document `processor_config` options in README.md

**Import chain (no circular imports):**
- `processors` → `bridge` (for interface + Register)
- `main` → `_ processors` (blank import, triggers init)
- `bridge` does NOT import `processors`

**ProcessResult semantics:**
- `{Drop: true}` — discard message, do not send to IRC
- `{Formatted: "..."}` — use this string (bridge applies SanitizeAndTruncate)
- `{}` — pass through to normal `FormatMessage` template path

### Adding a New Admin Command

1. Open `internal/admin/commands.go`
2. Add a new `case` in the `dispatch()` switch statement
3. Implement a `cmdXxx(client *girc.Client, replyTo string, args []string)` method on `*Handler`
4. Add any required method to the `BridgeAdmin` interface in `handler.go`
5. Implement the new method on `*Bridge` in `internal/bridge/bridge.go`
6. Add tests in `internal/admin/handler_test.go`
7. Update the `!help` output in `cmdHelp()`
8. Document in README.md

**Import chain (no circular imports):**
- `admin` defines `BridgeAdmin` interface — does NOT import `bridge`
- `bridge` exposes delegate methods — does NOT import `admin`
- `main` imports both and wires them together

### Adding a New Message Format Variable

1. Add field to template data map in `irc/formatter.go:FormatMessage()`
2. Update `types.Message` if new data needed
3. Add test cases in `formatter_test.go`
4. Document in README.md with example

### Debugging Connection Issues

1. Enable debug logging: `logging.level: "debug"` in config
2. Check component logs:
   - `component=mqtt` for MQTT issues
   - `component=irc` for IRC issues
   - `component=bridge` for mapping/processing
3. Check health endpoint: `curl localhost:8080/health`
4. Verify TLS settings match server requirements

## Code Style Conventions

### Logging

```go
// Use component field for log source
logger := logger.With().Str("component", "mqtt").Logger()

// Log structured data
logger.Info().
    Str("topic", topic).
    Int("payload_size", len(payload)).
    Msg("received message")

// Log levels:
// - Debug: Detailed flow, message content
// - Info: Lifecycle events, connections
// - Warn: Recoverable errors, reconnections
// - Error: Failed operations
```

### Error Handling

```go
// Wrap errors with context
return fmt.Errorf("failed to connect: %w", err)

// Log errors before returning
logger.Error().Err(err).Msg("operation failed")
return err
```

### Goroutine Patterns

```go
// Always use WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    // work
}()

// Always respect context
select {
case <-ctx.Done():
    return
case msg := <-queue:
    // process
}
```

## Known Limitations & Future Work

### Current Limitations

1. **One-Way Messages**: MQTT → IRC only for data messages (no IRC → MQTT); admin commands provide bridge control but not message routing
2. **Single MQTT Broker**: Cannot subscribe to multiple brokers
3. **No Metrics**: No Prometheus/StatsD integration
4. **Static Config**: Requires restart to change mappings

### Planned Enhancements (Phase 2+)

- [ ] Bidirectional bridging (IRC → MQTT)
- [ ] Prometheus metrics (`/metrics` endpoint)
- [ ] Message filtering with regex
- [ ] Multiple MQTT broker support
- [ ] Dynamic subscription via IRC commands (`!subscribe topic`)
- [ ] Hot config reload (SIGHUP)
- [ ] Message persistence during downtime
- [ ] Kubernetes manifests

### Not Planned (Out of Scope)

- GUI/web interface (CLI only)
- Message transformation/routing logic (keep simple)
- Support for IRC services other than NickServ
- MQTT v5 specific features (v3.1/3.1.1 sufficient)

## Extending the Project

### Adding a New Client Type (e.g., Slack, Discord)

1. Create `internal/slack/client.go` following IRC client pattern
2. Implement `SendMessage(ctx, channel, message)` method
3. Add config section: `SlackConfig` with webhook/token
4. Modify bridge to support multiple output targets
5. Update mapper to support multi-protocol routing

### Adding Metrics

1. Create `internal/metrics/collector.go`
2. Use `prometheus/client_golang` library
3. Add metrics:
   - `mqtt2irc_messages_total{topic, channel}`
   - `mqtt2irc_queue_size`
   - `mqtt2irc_connection_status{service}`
4. Add `/metrics` endpoint to health server
5. Update Dockerfile with metrics port

### Adding a New Processor Type

See "Adding a New Processor" in Common Tasks above. Example skeleton:

```go
package processors

import "github.com/dyuri/mqtt2irc/internal/bridge"
import "github.com/dyuri/mqtt2irc/pkg/types"

func init() { bridge.Register("myprocessor", newMyProcessor) }

type myProcessor struct{}

func newMyProcessor(cfg map[string]interface{}) (bridge.Processor, error) {
    return &myProcessor{}, nil
}

func (p *myProcessor) Process(msg types.Message) (bridge.ProcessResult, error) {
    // Return Drop:true to filter, Formatted:"..." to override, or {} to pass through.
    return bridge.ProcessResult{}, nil
}
```

## Development Workflow

### Setup

```bash
# Clone and build
git clone <repo>
cd mqtt2irc
make deps
make build

# Run tests
make test

# Start test environment
make docker-compose-up
./mqtt2irc -config configs/config.test.yaml
```

### Making Changes

1. Read this CLAUDE.md first
2. Check existing patterns in codebase
3. Write tests for new functionality
4. Update relevant documentation
5. Run `make test` before committing
6. Update IMPLEMENTATION_SUMMARY.md if significant

### Release Process

1. Update version in `main.go` or use `-ldflags`
2. Run full test suite: `make test`
3. Build for all platforms: `make build-all`
4. Build Docker image: `make docker-build`
5. Tag release: `git tag v1.0.0`
6. Update README.md with changelog

## Security Considerations

### Credentials

- **NEVER** commit real credentials to git
- Use environment variables for secrets
- `configs/config.yaml` is gitignored (only .example is tracked)
- NickServ passwords should use env vars: `MQTT2IRC_IRC_NICKSERV_PASSWORD`

### Message Safety

- All payloads are sanitized before IRC (see `formatter.go:sanitize()`)
- Control characters are stripped
- Messages are truncated to prevent overflow
- No shell execution or eval of payloads

### Network Security

- TLS enabled by default for both MQTT and IRC
- MinVersion set to TLS 1.2
- Certificate validation enabled
- No plaintext credentials in logs

### Admin Command Security

- Admin system is **disabled by default** (`admin.enabled: false`)
- `allow_list` is required when enabled; validation rejects empty lists
- All command attempts (authorized or not) are audit-logged with nick and host
- Hostmask glob uses `path.Match` (stdlib, no eval); patterns validated at startup
- `!shutdown` sends `SIGTERM` — reuses existing graceful shutdown, no new code path
- IRC authentication has inherent limitations: prefer hostmask auth over nick-only

## Troubleshooting Guide

### "Config File Not Found"

- Specify path: `./mqtt2irc -config configs/config.yaml`
- Or place `config.yaml` in current directory or `./configs/`

### MQTT Connection Fails

- Check broker URL scheme: `tcp://` or `ssl://`
- Verify `use_tls` matches broker setup
- Check credentials if broker requires auth
- Try QoS 0 if broker doesn't support QoS 1/2

### IRC Connection Fails

- Check TLS: port 6697 usually requires TLS, 6667 is plain
- Some servers require registered nicknames
- Check if server bans automated clients
- Try without NickServ first to isolate auth issues

### Messages Not Appearing

- Enable debug logging: `logging.level: "debug"`
- Check mapping patterns match topics exactly
- Verify bot joined IRC channels (check logs)
- Test with simple exact match first, then wildcards
- Publish test message: `mosquitto_pub -t test/topic -m hello`

### Bot Kicked from IRC

- Reduce `rate_limit.messages_per_second` (try 1.0)
- Check MQTT isn't publishing too frequently
- Verify queue isn't overflowing (check `/health`)
- Some servers have stricter limits

## Questions to Ask When Working on This Project

1. **Does this change affect message flow?** Update architecture diagram if yes.
2. **Does this need configuration?** Add to config structs and validation.
3. **Does this need tests?** Pure logic always needs tests.
4. **Does this change behavior?** Update README.md and examples.
5. **Does this add a dependency?** Justify why existing libs aren't sufficient.
6. **Is this backwards compatible?** Consider existing deployments.
7. **How does this handle errors?** Fail fast, log, or retry?
8. **How does this affect shutdown?** Respect context cancellation.

## Getting Help

- **README.md**: User-facing documentation, configuration reference
- **QUICKSTART.md**: 5-minute setup guide
- **IMPLEMENTATION_SUMMARY.md**: Technical implementation details
- **This file (CLAUDE.md)**: Architecture and development guide
- **Code comments**: Implementation-specific details
- **Tests**: Examples of correct usage

## Project Status

- **Current Phase**: Phase 1 (MVP) - ✅ COMPLETE
- **Production Ready**: Yes
- **Test Coverage**: Core modules (mapper, formatter, admin handler)
- **Documentation**: Complete
- **Last Updated**: 2026-02-26

---

**Remember**: This is a production bridge. Prioritize reliability, security, and simplicity over features. Every feature adds complexity - make sure it's worth it.
