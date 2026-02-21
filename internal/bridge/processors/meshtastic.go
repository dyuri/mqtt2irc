// Package processors provides message pre-processors for the mqtt2irc bridge.
// Import this package with a blank import to register all processors:
//
//	import _ "github.com/dyuri/mqtt2irc/internal/bridge/processors"
package processors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"text/template"
	"time"

	"github.com/dyuri/mqtt2irc/internal/bridge"
	"github.com/dyuri/mqtt2irc/pkg/types"
)

func init() {
	bridge.Register("meshtastic", newMeshtasticProcessor)
}

// defaultMeshtasticFormats are the built-in format strings for each Meshtastic message type.
// {{.smart_from}} resolves to: registry shortname > sender field (!xxxxxxxx) > numeric from.
var defaultMeshtasticFormats = map[string]string{
	"nodeinfo":  "📱 {{.smart_from}} - {{.longname}} ({{.hardware}})",
	"position":  "🌍 {{.smart_from}} @ {{.latitude_i}},{{.longitude_i}} alt={{.altitude}}m",
	"text":      "🖊️ {{.smart_from}}: {{.text}}",
	"telemetry": "📡 {{.smart_from}} bat={{.battery_level}}% air={{.air_util_tx}} channel={{.channel_utilization}}",
	"default":   "🗨 [{{.msgtype}}] from {{.smart_from}}: {{.payload}}",
}

type meshtasticProcessor struct {
	dedupWindow time.Duration
	idField     string
	typeField   string
	formats     map[string]*template.Template
	cache       *dedupCache
	nodes       *nodeRegistry
}

// newMeshtasticProcessor creates a Meshtastic processor from a config map.
func newMeshtasticProcessor(config map[string]interface{}) (bridge.Processor, error) {
	p := &meshtasticProcessor{
		dedupWindow: 30 * time.Second,
		idField:     "id",
		typeField:   "type",
		formats:     make(map[string]*template.Template),
	}

	if v, ok := config["dedup_window"]; ok {
		d, err := time.ParseDuration(fmt.Sprintf("%v", v))
		if err != nil {
			return nil, fmt.Errorf("meshtastic: invalid dedup_window %q: %w", v, err)
		}
		p.dedupWindow = d
	}
	if v, ok := config["id_field"]; ok {
		p.idField = fmt.Sprintf("%v", v)
	}
	if v, ok := config["type_field"]; ok {
		p.typeField = fmt.Sprintf("%v", v)
	}

	// Node registry — optional persistence via node_db path.
	nodeDBPath := ""
	if v, ok := config["node_db"]; ok {
		nodeDBPath = fmt.Sprintf("%v", v)
	}
	reg := newNodeRegistry(nodeDBPath)
	if err := reg.load(); err != nil {
		return nil, fmt.Errorf("meshtastic: failed to load node registry: %w", err)
	}
	p.nodes = reg

	// Start from defaults, then override with user-supplied formats.
	fmtStrings := make(map[string]string, len(defaultMeshtasticFormats))
	for k, v := range defaultMeshtasticFormats {
		fmtStrings[k] = v
	}
	if v, ok := config["formats"]; ok {
		if fm, ok := v.(map[string]interface{}); ok {
			for k, val := range fm {
				fmtStrings[k] = fmt.Sprintf("%v", val)
			}
		}
	}

	for name, tmplStr := range fmtStrings {
		tmpl, err := template.New(name).Option("missingkey=zero").Parse(tmplStr)
		if err != nil {
			return nil, fmt.Errorf("meshtastic: invalid format template %q: %w", name, err)
		}
		p.formats[name] = tmpl
	}

	p.cache = newDedupCache(p.dedupWindow)
	return p, nil
}

// Process handles a single MQTT message for the Meshtastic bridge.
func (p *meshtasticProcessor) Process(msg types.Message) (bridge.ProcessResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &raw); err != nil {
		// Not JSON — pass through to normal FormatMessage path.
		return bridge.ProcessResult{}, nil
	}

	// Deduplicate by message ID field.
	if id, ok := raw[p.idField]; ok && id != nil {
		if p.cache.seen(fmt.Sprintf("%v", id)) {
			return bridge.ProcessResult{Drop: true}, nil
		}
	}

	// Determine message type.
	msgType := ""
	if t, ok := raw[p.typeField]; ok && t != nil {
		msgType = fmt.Sprintf("%v", t)
	}

	// Build flat template data from nested JSON.
	data := flattenMeshtastic(raw, msgType)

	// Update node registry on nodeinfo messages.
	if msgType == "nodeinfo" {
		if fromStr, _ := data["from"].(string); fromStr != "" {
			rec := nodeRecord{UpdatedAt: time.Now()}
			rec.ShortName, _ = data["shortname"].(string)
			rec.LongName, _ = data["longname"].(string)
			// Non-fatal: in-memory registry is always updated; only disk write may fail.
			_ = p.nodes.update(fromStr, rec)
		}
	}

	// Add smart_from: registry shortname > sender field (!xxxxxxxx) > raw from.
	data["smart_from"] = p.smartFrom(data)

	// Select the best matching template.
	tmpl := p.selectTemplate(msgType)
	if tmpl == nil {
		return bridge.ProcessResult{}, nil
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return bridge.ProcessResult{}, fmt.Errorf("meshtastic: template execution failed: %w", err)
	}

	// Return the raw rendered string; bridge applies SanitizeAndTruncate.
	return bridge.ProcessResult{Formatted: buf.String()}, nil
}

// smartFrom resolves the best display name for a message sender.
//
// Priority:
//  1. shortname from the node registry (populated by nodeinfo messages)
//  2. sender field from the current message (!xxxxxxxx — always 9 chars)
//  3. raw from value (numeric node ID)
func (p *meshtasticProcessor) smartFrom(data map[string]interface{}) string {
	fromStr, _ := data["from"].(string)

	if rec, ok := p.nodes.get(fromStr); ok && rec.ShortName != "" {
		return rec.ShortName
	}
	if sender, _ := data["sender"].(string); sender != "" {
		return sender
	}
	return fromStr
}

// selectTemplate returns the template for msgType, or the "default" template, or nil.
func (p *meshtasticProcessor) selectTemplate(msgType string) *template.Template {
	if tmpl, ok := p.formats[msgType]; ok {
		return tmpl
	}
	if tmpl, ok := p.formats["default"]; ok {
		return tmpl
	}
	return nil
}

// flattenMeshtastic builds a flat map for template rendering from a Meshtastic JSON object.
//
// Flattening rules:
//  1. Top-level scalar fields are included (stringified for consistent template rendering).
//  2. The "payload" sub-object's fields are hoisted to the top level (overwriting scalars with
//     the same name, since payload fields are what users most often want to display).
//  3. Nested objects within "payload" are also hoisted one level deep.
//  4. "type" is renamed to "msgtype" to avoid collision with Go template internals.
func flattenMeshtastic(raw map[string]interface{}, msgType string) map[string]interface{} {
	data := make(map[string]interface{}, len(raw))

	// Step 1: top-level scalar fields.
	for k, v := range raw {
		if _, isMap := v.(map[string]interface{}); !isMap {
			data[k] = stringify(v)
		}
	}

	// Step 2: hoist fields from the "payload" sub-object.
	if payload, ok := raw["payload"]; ok {
		if pm, ok := payload.(map[string]interface{}); ok {
			for k, v := range pm {
				data[k] = stringify(v)
				// Step 3: hoist nested objects inside payload one level.
				if nested, ok := v.(map[string]interface{}); ok {
					for nk, nv := range nested {
						if _, exists := data[nk]; !exists {
							data[nk] = stringify(nv)
						}
					}
				}
			}
		}
	}

	// Step 4: rename "type" → "msgtype".
	data["msgtype"] = msgType
	delete(data, "type")

	return data
}

// stringify converts a JSON-decoded value to a human-readable string.
// float64 values that are whole numbers are formatted as integers to avoid
// scientific notation (e.g. 479000000 instead of 4.79e+08).
func stringify(v interface{}) string {
	switch val := v.(type) {
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

// --- node registry ---

// nodeRecord holds the known identity information for a Meshtastic node.
// It is populated from nodeinfo messages and persisted across restarts.
type nodeRecord struct {
	ShortName string    `json:"shortname,omitempty"`
	LongName  string    `json:"longname,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// nodeRegistry stores node identity associations keyed by the numeric node ID
// (the "from" field, stringified). When a node_db path is configured, the
// registry is loaded at startup and saved atomically after each update.
type nodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]nodeRecord
	path  string // empty = in-memory only, no persistence
}

func newNodeRegistry(path string) *nodeRegistry {
	return &nodeRegistry{
		nodes: make(map[string]nodeRecord),
		path:  path,
	}
}

// load reads the node registry from disk. No-op when path is empty or file does not exist.
func (r *nodeRegistry) load() error {
	if r.path == "" {
		return nil
	}
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil // fresh start; file will be created on first update
	}
	if err != nil {
		return fmt.Errorf("node registry: read %s: %w", r.path, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := json.Unmarshal(data, &r.nodes); err != nil {
		return fmt.Errorf("node registry: parse %s: %w", r.path, err)
	}
	return nil
}

// save writes the node registry to disk atomically (write temp + rename).
// No-op when path is empty.
func (r *nodeRegistry) save() error {
	if r.path == "" {
		return nil
	}
	r.mu.RLock()
	data, err := json.MarshalIndent(r.nodes, "", "  ")
	r.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("node registry: marshal: %w", err)
	}
	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("node registry: write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("node registry: rename to %s: %w", r.path, err)
	}
	return nil
}

// get returns the record for a node ID, if known.
func (r *nodeRegistry) get(from string) (nodeRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.nodes[from]
	return rec, ok
}

// update stores a node record in memory and persists to disk.
// The in-memory update always succeeds; a non-nil error indicates only that
// the disk write failed (the registry remains correct in memory).
func (r *nodeRegistry) update(from string, rec nodeRecord) error {
	r.mu.Lock()
	r.nodes[from] = rec
	r.mu.Unlock()
	return r.save()
}

// --- dedup cache ---

type dedupCache struct {
	mu      sync.Mutex
	entries map[string]time.Time // id → expiry time
	window  time.Duration
}

func newDedupCache(window time.Duration) *dedupCache {
	return &dedupCache{
		entries: make(map[string]time.Time),
		window:  window,
	}
}

// seen returns true if id was observed within the dedup window.
// Lazily evicts expired entries on each call.
func (c *dedupCache) seen(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Lazy eviction of expired entries.
	for k, expiry := range c.entries {
		if now.After(expiry) {
			delete(c.entries, k)
		}
	}

	if expiry, ok := c.entries[id]; ok && now.Before(expiry) {
		return true
	}

	c.entries[id] = now.Add(c.window)
	return false
}
