# Per-Mapping Message Processor System

## Context

The bridge currently applies a single Go template (`message_format`) to every message matched by a mapping. This is insufficient for heterogeneous MQTT sources like Meshtastic, where:
- Messages carry a `type` field that determines their semantic meaning (nodeinfo, position, text, telemetry, etc.)
- Duplicate messages arrive with the same `id` and should be suppressed within a time window
- Each message type deserves its own format string rather than one generic template

The goal is to add an optional **processor** hook per mapping. A processor can filter (drop) or pre-format a message before the normal `FormatMessage` path runs. Processors are named, registered at startup, and configured with a free-form `processor_config` map.

---

## Design

### Processor Interface (`internal/bridge/processor.go` — NEW)

```go
type ProcessResult struct {
    Drop      bool   // true = discard, do not send to IRC
    Formatted string // if non-empty, use this as the IRC message (skips FormatMessage)
}

type Processor interface {
    Process(msg types.Message) (ProcessResult, error)
}

type ProcessorFactory func(config map[string]interface{}) (Processor, error)
```

A global registry maps processor names to factories:
```go
var registry = map[string]ProcessorFactory{}

func Register(name string, factory ProcessorFactory) { registry[name] = factory }
func New(name string, config map[string]interface{}) (Processor, error) { ... }
```

### Config Extension (`internal/config/config.go`)

Add two fields to `MappingConfig`:
```go
type MappingConfig struct {
    MQTTTopic       string                 `mapstructure:"mqtt_topic"`
    IRCChannels     []string               `mapstructure:"irc_channels"`
    MessageFormat   string                 `mapstructure:"message_format"`
    Processor       string                 `mapstructure:"processor"`        // optional: processor name
    ProcessorConfig map[string]interface{} `mapstructure:"processor_config"` // optional: processor config
}
```

### Validation (`internal/config/validation.go`)

If `processor` is set, verify it is a registered name. Called after processor registration happens at startup.

### Bridge Wiring (`internal/bridge/bridge.go`)

In `New()`, after creating the mapper, instantiate a processor for each mapping that has one:
```go
type mappingProcessor struct {
    mapping   config.MappingConfig
    processor Processor // nil if no processor configured
}
```
Store `[]mappingProcessor` instead of relying purely on the mapper result.

In `handleMessage()`, after mapping lookup:
```go
if mp.processor != nil {
    result, err := mp.processor.Process(msg)
    if err != nil { log error }
    if result.Drop { continue }
    if result.Formatted != "" {
        // sanitize + truncate, then SendMessage directly
        continue
    }
}
// fall through to normal FormatMessage
```

---

## Meshtastic Processor (`internal/bridge/processors/meshtastic.go` — NEW)

### Config (via `processor_config`)
```yaml
processor: "meshtastic"
processor_config:
  dedup_window: "30s"       # duration string; messages with same id within window are dropped
  id_field: "id"            # JSON field used for dedup (default: "id")
  type_field: "type"        # JSON field for message type (default: "type")
  formats:
    nodeinfo:  "Node {{.from}} - {{.longName}} (hw: {{.hwModel}})"
    position:  "{{.from}} @ lat={{.latitudeI}} lon={{.longitudeI}} alt={{.altitude}}m"
    text:      "{{.from}}: {{.text}}"
    telemetry: "{{.from}} bat={{.batteryLevel}}%"
    default:   "[{{.msgtype}}] from {{.from}}"
```

### Template Data
The processor flattens the Meshtastic JSON into a single map passed to the selected format template:
- Top-level fields: `from`, `to`, `id`, `msgtype` (= the `type` value, renamed to avoid collision)
- `payload` sub-object: fields hoisted to top level (e.g. `latitudeI`, `text`, `longName`, `hwModel`, `batteryLevel`)
- For nested sub-objects in payload (e.g. `user`, `deviceMetrics`), fields are hoisted one level

### Dedup Cache
```go
type dedupCache struct {
    mu      sync.Mutex
    entries map[string]time.Time // id → expiry time
    window  time.Duration
}
```
- `seen(id string) bool`: returns true if id seen within window; records/refreshes entry; lazily evicts expired entries on each check (iterate map, delete expired)
- No extra goroutine needed

### Format Template Execution
Uses `text/template` with `missingkey=zero`, same as the existing formatter. After rendering, result is passed through the existing `sanitize()` and `truncate()` from `internal/irc/formatter.go` (both are already exported-accessible from within the package; or we expose helpers).

Actually `sanitize` and `truncate` are unexported. Two options:
- Export them from `irc` package: `Sanitize`, `Truncate`
- Duplicate the call: have the bridge apply sanitize+truncate via a new exported `SanitizeAndTruncate(s string, maxLen int, suffix string) string` helper

**Chosen**: Add `SanitizeAndTruncate(s, maxLen, suffix)` exported helper to `internal/irc/formatter.go` — one clean entry point.

---

## Critical Files

| File | Change |
|------|--------|
| `internal/bridge/processor.go` | NEW — interface, registry |
| `internal/bridge/processors/meshtastic.go` | NEW — meshtastic processor |
| `internal/bridge/bridge.go` | Instantiate processors in `New()`, call in `handleMessage()` |
| `internal/config/config.go` | Add `Processor`, `ProcessorConfig` to `MappingConfig` |
| `internal/config/validation.go` | Validate processor name if set |
| `internal/irc/formatter.go` | Export `SanitizeAndTruncate()` helper |
| `configs/config.example.yaml` | Add commented meshtastic example |

---

## Example Config

```yaml
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

---

## Processor Registration

In `internal/bridge/processors/meshtastic.go`, use `func init()` to register:
```go
func init() {
    processor.Register("meshtastic", NewMeshtasticProcessor)
}
```

Import the `processors` package from `bridge.go` with a blank import to trigger registration before `New()` runs:
```go
import _ "github.com/dyuri/mqtt2irc/internal/bridge/processors"
```

---

## Verification

1. `go test ./...` — all existing tests still pass
2. Add unit test for Meshtastic processor:
   - `TestMeshtasticProcessor_Dedup`: same id within window → Drop=true
   - `TestMeshtasticProcessor_TypeRouting`: nodeinfo → uses nodeinfo format
   - `TestMeshtasticProcessor_DefaultFormat`: unknown type → uses default format
3. Manual: configure Meshtastic mapping, publish sample Meshtastic JSON via `mosquitto_pub`, confirm correct IRC output and duplicates suppressed
