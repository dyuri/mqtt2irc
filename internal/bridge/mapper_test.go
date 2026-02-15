package bridge

import (
	"testing"

	"github.com/dyuri/mqtt2irc/internal/config"
)

func TestMatchTopic(t *testing.T) {
	mapper := NewMapper([]config.MappingConfig{})

	tests := []struct {
		name     string
		topic    string
		pattern  string
		expected bool
	}{
		// Exact matches
		{"exact match", "sensors/temp", "sensors/temp", true},
		{"no match", "sensors/temp", "sensors/humidity", false},

		// Single-level wildcard (+)
		{"+ match", "sensors/bedroom/temp", "sensors/+/temp", true},
		{"+ no match", "sensors/bedroom/bathroom/temp", "sensors/+/temp", false},
		{"+ middle", "a/b/c", "a/+/c", true},

		// Multi-level wildcard (#)
		{"# match all", "sensors/bedroom/temp", "sensors/#", true},
		{"# match deep", "sensors/bedroom/bathroom/temp", "sensors/#", true},
		{"# at end", "a/b/c", "a/b/#", true},
		{"# match single", "sensors/temp", "sensors/#", true},

		// Combined wildcards
		{"+ and # combined", "sensors/bedroom/temp/reading", "sensors/+/temp/#", true},
		{"+ and # no match", "sensors/temp/reading", "sensors/+/temp/#", false},

		// Edge cases
		{"empty pattern", "sensors/temp", "", false},
		{"root #", "anything/deep/path", "#", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapper.matchTopic(tt.topic, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchTopic(%q, %q) = %v, want %v",
					tt.topic, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestIsValidPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		{"simple topic", "sensors/temp", true},
		{"with +", "sensors/+/temp", true},
		{"with #", "sensors/#", true},
		{"# at end only", "sensors/temp/#", true},
		{"# in middle", "sensors/#/temp", false},
		{"multiple #", "sensors/#/#", false},
		{"+ mixed", "sensors/+temp", false},
		{"# mixed", "sensors/#temp", false},
		{"empty", "", false},
		{"path traversal", "sensors/../temp", false},
		{"empty level", "sensors//temp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidPattern(tt.pattern)
			if result != tt.expected {
				t.Errorf("IsValidPattern(%q) = %v, want %v",
					tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestMap(t *testing.T) {
	mappings := []config.MappingConfig{
		{
			MQTTTopic:     "sensors/temp/#",
			IRCChannels:   []string{"#sensors"},
			MessageFormat: "Temp: {{.Payload}}",
		},
		{
			MQTTTopic:     "alerts/critical",
			IRCChannels:   []string{"#alerts", "#ops"},
			MessageFormat: "ALERT: {{.Payload}}",
		},
		{
			MQTTTopic:     "sensors/+/humidity",
			IRCChannels:   []string{"#humidity"},
			MessageFormat: "Humidity: {{.Payload}}",
		},
	}

	mapper := NewMapper(mappings)

	tests := []struct {
		name          string
		topic         string
		expectedCount int
		firstChannel  string
	}{
		{"match temp", "sensors/temp/bedroom", 1, "#sensors"},
		{"match critical", "alerts/critical", 1, "#alerts"},
		{"match humidity", "sensors/bedroom/humidity", 1, "#humidity"},
		{"no match", "random/topic", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := mapper.Map(tt.topic)
			if len(results) != tt.expectedCount {
				t.Errorf("Map(%q) returned %d results, want %d",
					tt.topic, len(results), tt.expectedCount)
			}
			if tt.expectedCount > 0 && len(results) > 0 {
				if results[0].IRCChannels[0] != tt.firstChannel {
					t.Errorf("Map(%q) first channel = %q, want %q",
						tt.topic, results[0].IRCChannels[0], tt.firstChannel)
				}
			}
		})
	}
}
