package irc

import (
	"strings"
	"testing"
	"time"

	"github.com/dyuri/mqtt2irc/pkg/types"
)

func TestFormatMessage(t *testing.T) {
	msg := types.Message{
		Topic:     "sensors/temp",
		Payload:   []byte("25.5"),
		Timestamp: time.Now(),
		QoS:       1,
	}

	tests := []struct {
		name           string
		template       string
		maxLength      int
		truncateSuffix string
		expected       string
	}{
		{
			name:           "default template",
			template:       "",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "[sensors/temp] 25.5",
		},
		{
			name:           "custom template",
			template:       "{{.Topic}}: {{.Payload}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "sensors/temp: 25.5",
		},
		{
			name:           "template with QoS",
			template:       "[QoS{{.QoS}}] {{.Payload}}",
			maxLength:      100,
			truncateSuffix: "...",
			expected:       "[QoS1] 25.5",
		},
		{
			name:           "truncation",
			template:       "{{.Payload}}",
			maxLength:      3,
			truncateSuffix: "...",
			expected:       "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FormatMessage(msg, tt.template, tt.maxLength, tt.truncateSuffix)
			if err != nil {
				t.Errorf("FormatMessage() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("FormatMessage() = %q, want %q", result, tt.expected)
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
