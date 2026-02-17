package irc

import (
	"strings"
	"testing"
	"time"

	"github.com/dyuri/mqtt2irc/pkg/types"
)

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name           string
		msg            types.Message
		template       string
		maxLength      int
		truncateSuffix string
		expected       string
	}{
		{
			name:           "default template",
			msg:            types.Message{Topic: "sensors/temp", Payload: []byte("25.5"), QoS: 1},
			template:       "",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "[sensors/temp] 25.5",
		},
		{
			name:           "custom template",
			msg:            types.Message{Topic: "sensors/temp", Payload: []byte("25.5"), QoS: 1},
			template:       "{{.Topic}}: {{.Payload}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "sensors/temp: 25.5",
		},
		{
			name:           "template with QoS",
			msg:            types.Message{Topic: "sensors/temp", Payload: []byte("25.5"), QoS: 1},
			template:       "[QoS{{.QoS}}] {{.Payload}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "[QoS1] 25.5",
		},
		{
			name:           "truncation",
			msg:            types.Message{Topic: "sensors/temp", Payload: []byte("25.5"), QoS: 1},
			template:       "{{.Payload}}",
			maxLength:      3,
			truncateSuffix: "...",
			expected:       "...",
		},
		{
			name:           "json field access",
			msg:            types.Message{Topic: "sensors/env", Payload: []byte(`{"temp":22.5,"unit":"C"}`), QoS: 1},
			template:       "{{.Topic}}: temp={{.JSON.temp}}{{.JSON.unit}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "sensors/env: temp=22.5C",
		},
		{
			name:           "json missing field returns empty",
			msg:            types.Message{Topic: "sensors/env", Payload: []byte(`{"temp":22.5}`), QoS: 1},
			template:       "{{.JSON.temp}} {{.JSON.missing}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "22.5",
		},
		{
			name:           "non-json payload JSON is nil",
			msg:            types.Message{Topic: "sensors/temp", Payload: []byte("25.5"), QoS: 1},
			template:       "{{.Payload}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "25.5",
		},
		{
			name:           "binary payload",
			msg:            types.Message{Topic: "sensors/raw", Payload: []byte{0x89, 0x50, 0x4E, 0x47}, QoS: 1},
			template:       "{{.Topic}}: {{.Payload}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "sensors/raw: [binary data, 4 bytes]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.msg.Timestamp = time.Now()
			result, err := FormatMessage(tt.msg, tt.template, tt.maxLength, tt.truncateSuffix)
			if err != nil {
				t.Errorf("FormatMessage() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("FormatMessage() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantNil bool
		wantKey string
		wantVal string
	}{
		{"valid object", []byte(`{"temp":22.5,"unit":"C"}`), false, "temp", "22.5"},
		{"nested object", []byte(`{"device":{"name":"sensor1"}}`), false, "device", "map[name:sensor1]"},
		{"array", []byte(`[1,2,3]`), true, "", ""},
		{"scalar string", []byte(`"hello"`), true, "", ""},
		{"scalar number", []byte(`42`), true, "", ""},
		{"invalid json", []byte(`not json`), true, "", ""},
		{"empty", []byte{}, true, "", ""},
		{"binary", []byte{0xFF, 0xFE}, true, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseJSON(tt.input)
			if tt.wantNil {
				if result != nil {
					t.Errorf("parseJSON() = %v, want nil", result)
				}
				return
			}
			if result == nil {
				t.Fatal("parseJSON() = nil, want non-nil")
			}
			if tt.wantKey != "" {
				val, ok := result[tt.wantKey]
				if !ok {
					t.Errorf("parseJSON() missing key %q", tt.wantKey)
				} else if val != tt.wantVal {
					t.Errorf("parseJSON()[%q] = %q, want %q", tt.wantKey, val, tt.wantVal)
				}
			}
		})
	}
}

func TestPayloadString(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{"valid utf8", []byte("hello"), "hello"},
		{"empty", []byte{}, ""},
		{"valid utf8 multibyte", []byte("hello 世界"), "hello 世界"},
		{"binary single byte", []byte{0xFF}, "[binary data, 1 bytes]"},
		{"binary multi byte", []byte{0x89, 0x50, 0x4E, 0x47}, "[binary data, 4 bytes]"},
		{"invalid utf8 sequence", []byte{0xc3, 0x28}, "[binary data, 2 bytes]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := payloadString(tt.input)
			if result != tt.expected {
				t.Errorf("payloadString(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal text", "hello world", "hello world"},
		{"multiple spaces", "hello    world", "hello world"},
		{"control chars", "hello\x00world", "hello world"},
		{"newlines", "hello\nworld", "hello world"},
		{"tabs", "hello\tworld", "hello world"},
		{"utf8", "hello 世界", "hello 世界"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("sanitize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		suffix    string
		wantLen   int
	}{
		{"no truncation", "hello", 10, "...", 5},
		{"exact length", "hello", 5, "...", 5},
		{"needs truncation", "hello world", 8, "...", 8},
		{"utf8 truncation", "hello 世界", 8, "...", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLength, tt.suffix)
			// Check length in runes
			runeCount := len([]rune(result))
			if runeCount > tt.maxLength {
				t.Errorf("truncate(%q, %d) length = %d, want <= %d",
					tt.input, tt.maxLength, runeCount, tt.maxLength)
			}
			if tt.maxLength < len([]rune(tt.input)) {
				// Should have suffix
				if !strings.HasSuffix(result, tt.suffix) {
					t.Errorf("truncate(%q, %d) = %q, should end with %q",
						tt.input, tt.maxLength, result, tt.suffix)
				}
			}
		})
	}
}
