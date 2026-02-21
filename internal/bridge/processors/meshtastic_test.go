package processors

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dyuri/mqtt2irc/pkg/types"
)

func makeMsg(payload interface{}) types.Message {
	b, _ := json.Marshal(payload)
	return types.Message{Topic: "test/topic", Payload: b}
}

func TestMeshtasticProcessor_Dedup(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{
		"dedup_window": "1m",
	})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := makeMsg(map[string]interface{}{
		"id":   12345,
		"type": "text",
		"from": 111111,
		"payload": map[string]interface{}{
			"text": "hello",
		},
	})

	// First occurrence: should be forwarded.
	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if result.Drop {
		t.Error("first occurrence should not be dropped")
	}

	// Second occurrence within window: should be dropped.
	result, err = p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !result.Drop {
		t.Error("duplicate within window should be dropped")
	}
}

func TestMeshtasticProcessor_Dedup_Expiry(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{
		"dedup_window": "50ms",
	})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := makeMsg(map[string]interface{}{
		"id":      99,
		"type":    "text",
		"from":    222222,
		"payload": map[string]interface{}{"text": "hi"},
	})

	result, _ := p.Process(msg)
	if result.Drop {
		t.Error("first occurrence should not be dropped")
	}

	// Wait for the dedup window to expire.
	time.Sleep(100 * time.Millisecond)

	result, _ = p.Process(msg)
	if result.Drop {
		t.Error("message after window expiry should not be dropped")
	}
}

func TestMeshtasticProcessor_TypeRouting(t *testing.T) {
	p, err := newMeshtasticProcessor(map[string]interface{}{})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	tests := []struct {
		name     string
		payload  map[string]interface{}
		contains string
	}{
		{
			name: "nodeinfo",
			payload: map[string]interface{}{
				"id":   1,
				"type": "nodeinfo",
				"from": 111,
				"payload": map[string]interface{}{
					"longName": "Alice",
					"hwModel":  "HELTEC_V3",
				},
			},
			contains: "Alice",
		},
		{
			name: "position",
			payload: map[string]interface{}{
				"id":   2,
				"type": "position",
				"from": 222,
				"payload": map[string]interface{}{
					"latitudeI":  479000000,
					"longitudeI": 190000000,
					"altitude":   150,
				},
			},
			contains: "479000000",
		},
		{
			name: "text",
			payload: map[string]interface{}{
				"id":   3,
				"type": "text",
				"from": 333,
				"payload": map[string]interface{}{
					"text": "hello world",
				},
			},
			contains: "hello world",
		},
		{
			name: "telemetry",
			payload: map[string]interface{}{
				"id":   4,
				"type": "telemetry",
				"from": 444,
				"payload": map[string]interface{}{
					"batteryLevel": 85,
				},
			},
			contains: "85",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := makeMsg(tt.payload)
			result, err := p.Process(msg)
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

	msg := makeMsg(map[string]interface{}{
		"id":   5,
		"type": "unknowntype",
		"from": 555,
	})

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
	if !containsStr(result.Formatted, "555") {
		t.Errorf("default format should contain from field, got %q", result.Formatted)
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
			"text": "MSG from {{.from}}: {{.text}}",
		},
	})
	if err != nil {
		t.Fatalf("newMeshtasticProcessor: %v", err)
	}

	msg := makeMsg(map[string]interface{}{
		"id":      6,
		"type":    "text",
		"from":    666,
		"payload": map[string]interface{}{"text": "custom"},
	})

	result, err := p.Process(msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if !containsStr(result.Formatted, "MSG from") {
		t.Errorf("expected custom format, got %q", result.Formatted)
	}
	if !containsStr(result.Formatted, "custom") {
		t.Errorf("expected text field in output, got %q", result.Formatted)
	}
}

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
