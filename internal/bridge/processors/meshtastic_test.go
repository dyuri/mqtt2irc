package processors

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dyuri/mqtt2irc/pkg/types"
)

func makeMsg(payload interface{}) types.Message {
	b, _ := json.Marshal(payload)
	return types.Message{Topic: "test/topic", Payload: b}
}

// meshtasticMsg builds a realistic Meshtastic MQTT message.
// sender is the !xxxxxxxx hex representation of from.
func meshtasticMsg(id int, msgType string, from int, sender string, payload map[string]interface{}) types.Message {
	m := map[string]interface{}{
		"id":      id,
		"type":    msgType,
		"from":    from,
		"sender":  sender,
		"channel": 0,
		"payload": payload,
	}
	return makeMsg(m)
}

// --- dedup ---

func TestMeshtasticProcessor_Dedup(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{"dedup_window": "1m"})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := meshtasticMsg(12345, "text", 111111, "!01b207cf", map[string]interface{}{"text": "hello"})

	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if result.Drop {
		t.Error("first occurrence should not be dropped")
	}

	result, err = p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !result.Drop {
		t.Error("duplicate within window should be dropped")
	}
}

func TestMeshtasticProcessor_Dedup_Expiry(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{"dedup_window": "50ms"})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := meshtasticMsg(99, "text", 222222, "!000036b2", map[string]interface{}{"text": "hi"})

	result, _ := p.Process(msg)
	if result.Drop {
		t.Error("first occurrence should not be dropped")
	}

	time.Sleep(100 * time.Millisecond)

	result, _ = p.Process(msg)
	if result.Drop {
		t.Error("message after window expiry should not be dropped")
	}
}

// --- type routing (field names match real Meshtastic snake_case MQTT JSON) ---

func TestMeshtasticProcessor_TypeRouting(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	tests := []struct {
		name     string
		msg      types.Message
		contains string
	}{
		{
			name: "nodeinfo",
			msg: meshtasticMsg(1, "nodeinfo", 111, "!0000006f", map[string]interface{}{
				"longname":  "Alice",
				"shortname": "ALI",
				"hardware":  "HELTEC_V3",
			}),
			contains: "Alice",
		},
		{
			name: "position",
			msg: meshtasticMsg(2, "position", 222, "!000000de", map[string]interface{}{
				"latitude_i":  479000000,
				"longitude_i": 190000000,
				"altitude":    150,
			}),
			contains: "479000000",
		},
		{
			name: "text",
			msg: meshtasticMsg(3, "text", 333, "!0000014d", map[string]interface{}{
				"text": "hello world",
			}),
			contains: "hello world",
		},
		{
			name: "telemetry",
			msg: meshtasticMsg(4, "telemetry", 444, "!000001bc", map[string]interface{}{
				"battery_level":       85,
				"air_util_tx":         3.14,
				"channel_utilization": 12,
			}),
			contains: "85",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Process(tt.msg)
			if err != nil {
				t.Fatalf("Process error: %v", err)
			}
			if result.Drop {
				t.Error("message should not be dropped")
			}
			if result.Formatted == "" {
				t.Error("Formatted should be non-empty")
			}
			if !containsStr(result.Formatted, tt.contains) {
				t.Errorf("Formatted %q does not contain %q", result.Formatted, tt.contains)
			}
		})
	}
}

func TestMeshtasticProcessor_DefaultFormat(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := meshtasticMsg(5, "unknowntype", 555, "!0000022b", nil)

	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if result.Drop {
		t.Error("message should not be dropped")
	}
	if !containsStr(result.Formatted, "unknowntype") {
		t.Errorf("default format should contain message type, got %q", result.Formatted)
	}
	// sender is present, so smart_from resolves to "!0000022b"
	if !containsStr(result.Formatted, "!0000022b") {
		t.Errorf("default format should contain sender, got %q", result.Formatted)
	}
}

func TestMeshtasticProcessor_NonJSON(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := types.Message{Topic: "test", Payload: []byte("plain text payload")}
	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if result.Drop {
		t.Error("non-JSON should not be dropped")
	}
	if result.Formatted != "" {
		t.Error("non-JSON should produce empty Formatted (fall through to FormatMessage)")
	}
}

func TestMeshtasticProcessor_CustomFormats(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{
		"formats": map[string]interface{}{
			"text": "MSG {{.smart_from}}: {{.text}}",
		},
	})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := meshtasticMsg(6, "text", 666, "!0000029a", map[string]interface{}{"text": "custom"})

	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !containsStr(result.Formatted, "MSG") {
		t.Errorf("expected custom format, got %q", result.Formatted)
	}
	if !containsStr(result.Formatted, "custom") {
		t.Errorf("expected text field in output, got %q", result.Formatted)
	}
}

// --- smart_from ---

func TestMeshtasticProcessor_SmartFrom_SenderFallback(t *testing.T) {
	// No registry entry; smart_from should resolve to sender field.
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := meshtasticMsg(7, "text", 777, "!00000309", map[string]interface{}{"text": "hi"})

	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !containsStr(result.Formatted, "!00000309") {
		t.Errorf("expected sender in output when no registry entry, got %q", result.Formatted)
	}
}

func TestMeshtasticProcessor_SmartFrom_FromFallback(t *testing.T) {
	// No registry entry and no sender field; smart_from should fall back to numeric from.
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	// Message without a sender field.
	raw := map[string]interface{}{
		"id":   8,
		"type": "text",
		"from": 888,
		"payload": map[string]interface{}{
			"text": "no sender",
		},
	}
	msg := makeMsg(raw)

	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !containsStr(result.Formatted, "888") {
		t.Errorf("expected from value in output when no sender, got %q", result.Formatted)
	}
}

func TestMeshtasticProcessor_SmartFrom_RegistryShortname(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	// Send a nodeinfo message to populate the registry.
	nodeinfo := meshtasticMsg(9, "nodeinfo", 999, "!000003e7", map[string]interface{}{
		"shortname": "BOB",
		"longname":  "Bob's Node",
		"hardware":  "TBEAM",
	})
	_, err = p.Process(nodeinfo)
	if err != nil {
		t.Fatalf("nodeinfo Process error: %v", err)
	}

	// Subsequent message from same node should use shortname.
	text := meshtasticMsg(10, "text", 999, "!000003e7", map[string]interface{}{"text": "greet"})
	result, err := p.Process(text)
	if err != nil {
		t.Fatalf("text Process error: %v", err)
	}
	if !containsStr(result.Formatted, "BOB") {
		t.Errorf("expected shortname BOB from registry, got %q", result.Formatted)
	}
}

func TestMeshtasticProcessor_SmartFrom_RegistryUpdate(t *testing.T) {
	// Verify that a second nodeinfo for the same node updates the shortname.
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	first := meshtasticMsg(11, "nodeinfo", 1111, "!00000457", map[string]interface{}{
		"shortname": "OLD",
		"longname":  "Old Name",
		"hardware":  "TBEAM",
	})
	p.Process(first) //nolint:errcheck

	second := meshtasticMsg(12, "nodeinfo", 1111, "!00000457", map[string]interface{}{
		"shortname": "NEW",
		"longname":  "New Name",
		"hardware":  "TBEAM",
	})
	p.Process(second) //nolint:errcheck

	text := meshtasticMsg(13, "text", 1111, "!00000457", map[string]interface{}{"text": "check"})
	result, err := p.Process(text)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !containsStr(result.Formatted, "NEW") {
		t.Errorf("expected updated shortname NEW, got %q", result.Formatted)
	}
	if containsStr(result.Formatted, "OLD") {
		t.Errorf("stale shortname OLD should not appear, got %q", result.Formatted)
	}
}

// --- node registry ---

func TestNodeRegistry_GetUpdate(t *testing.T) {
	r := newNodeRegistry("")

	_, ok := r.get("123")
	if ok {
		t.Error("get on empty registry should return false")
	}

	rec := nodeRecord{ShortName: "ALI", LongName: "Alice", UpdatedAt: time.Now()}
	if err := r.update("123", rec); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, ok := r.get("123")
	if !ok {
		t.Fatal("get should return true after update")
	}
	if got.ShortName != "ALI" {
		t.Errorf("ShortName = %q, want ALI", got.ShortName)
	}
	if got.LongName != "Alice" {
		t.Errorf("LongName = %q, want Alice", got.LongName)
	}
}

func TestNodeRegistry_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.json")

	// Write registry to disk.
	r1 := newNodeRegistry(path)
	if err := r1.load(); err != nil {
		t.Fatalf("load (empty): %v", err)
	}
	r1.update("42", nodeRecord{ShortName: "X", LongName: "Xray", UpdatedAt: time.Now()})   //nolint:errcheck
	r1.update("99", nodeRecord{ShortName: "Y", LongName: "Yankee", UpdatedAt: time.Now()}) //nolint:errcheck

	// New registry instance loads from same path.
	r2 := newNodeRegistry(path)
	if err := r2.load(); err != nil {
		t.Fatalf("load (existing): %v", err)
	}

	for _, tc := range []struct{ from, short, long string }{
		{"42", "X", "Xray"},
		{"99", "Y", "Yankee"},
	} {
		rec, ok := r2.get(tc.from)
		if !ok {
			t.Errorf("node %s not found after reload", tc.from)
			continue
		}
		if rec.ShortName != tc.short {
			t.Errorf("node %s: ShortName = %q, want %q", tc.from, rec.ShortName, tc.short)
		}
		if rec.LongName != tc.long {
			t.Errorf("node %s: LongName = %q, want %q", tc.from, rec.LongName, tc.long)
		}
	}
}

func TestNodeRegistry_PersistenceWithProcessor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.json")

	// First processor instance learns node info.
	p1, err := newMeshtasticProcessor(map[string]interface{}{"node_db": path})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}
	p1.Process(meshtasticMsg(1, "nodeinfo", 500, "!000001f4", map[string]interface{}{ //nolint:errcheck
		"shortname": "PRS",
		"longname":  "Persistent Node",
		"hardware":  "TBEAM",
	}))

	// Second processor instance (simulating restart) should have the shortname.
	p2, err := newMeshtasticProcessor(map[string]interface{}{"node_db": path})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor (reload): %v", err)
	}
	result, err := p2.Process(meshtasticMsg(2, "text", 500, "!000001f4", map[string]interface{}{"text": "after restart"}))
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !containsStr(result.Formatted, "PRS") {
		t.Errorf("expected PRS from persisted registry after restart, got %q", result.Formatted)
	}
}

func TestNodeRegistry_MissingFile(t *testing.T) {
	// A non-existent file should not be an error (fresh start).
	r := newNodeRegistry(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err := r.load(); err != nil {
		t.Errorf("load of missing file should not error, got: %v", err)
	}
}

// --- dedup cache ---

func TestDedupCache(t *testing.T) {
	c := newDedupCache(100 * time.Millisecond)

	if c.seen("abc") {
		t.Error("first call should return false")
	}
	if !c.seen("abc") {
		t.Error("second call within window should return true")
	}

	time.Sleep(150 * time.Millisecond)

	if c.seen("abc") {
		t.Error("call after window expiry should return false")
	}
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
