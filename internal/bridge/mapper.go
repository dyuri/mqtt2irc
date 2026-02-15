package bridge

import (
	"path"
	"strings"

	"github.com/dyuri/mqtt2irc/internal/config"
)

// Mapper handles topic-to-channel mapping
type Mapper struct {
	mappings []config.MappingConfig
}

// NewMapper creates a new topic mapper
func NewMapper(mappings []config.MappingConfig) *Mapper {
	return &Mapper{
		mappings: mappings,
	}
}

// MatchedMapping represents a matched mapping with its configuration
type MatchedMapping struct {
	IRCChannels   []string
	MessageFormat string
}

// Map finds all IRC channels and formats for a given MQTT topic
func (m *Mapper) Map(topic string) []MatchedMapping {
	var results []MatchedMapping

	for _, mapping := range m.mappings {
		if m.matchTopic(topic, mapping.MQTTTopic) {
			results = append(results, MatchedMapping{
				IRCChannels:   mapping.IRCChannels,
				MessageFormat: mapping.MessageFormat,
			})
		}
	}

	return results
}

// matchTopic checks if an MQTT topic matches a pattern
// Supports MQTT wildcards: + (single level), # (multi level)
func (m *Mapper) matchTopic(topic, pattern string) bool {
	// Exact match
	if topic == pattern {
		return true
	}

	// No wildcards - no match
	if !strings.Contains(pattern, "+") && !strings.Contains(pattern, "#") {
		return false
	}

	topicParts := strings.Split(topic, "/")
	patternParts := strings.Split(pattern, "/")

	return m.matchParts(topicParts, patternParts)
}

// matchParts recursively matches topic parts against pattern parts
func (m *Mapper) matchParts(topicParts, patternParts []string) bool {
	// If pattern is empty, topic must be empty too
	if len(patternParts) == 0 {
		return len(topicParts) == 0
	}

	// If pattern has # at this position, it matches everything remaining
	if patternParts[0] == "#" {
		// # must be the last element in pattern
		return len(patternParts) == 1
	}

	// If topic is empty but pattern isn't (and first isn't #), no match
	if len(topicParts) == 0 {
		return false
	}

	// If pattern has + at this position, it matches any single level
	if patternParts[0] == "+" {
		return m.matchParts(topicParts[1:], patternParts[1:])
	}

	// Exact match required at this level
	if topicParts[0] == patternParts[0] {
		return m.matchParts(topicParts[1:], patternParts[1:])
	}

	return false
}

// IsValidPattern checks if a pattern is valid MQTT topic pattern
func IsValidPattern(pattern string) bool {
	if pattern == "" {
		return false
	}

	// Check for path traversal attempts
	if strings.Contains(pattern, "..") {
		return false
	}

	parts := strings.Split(pattern, "/")
	hashSeen := false

	for i, part := range parts {
		// Empty parts are not allowed (except for shared subscriptions which we don't support)
		if part == "" {
			return false
		}

		// # can only appear as the last part
		if part == "#" {
			if i != len(parts)-1 {
				return false
			}
			hashSeen = true
		}

		// # can only appear once
		if hashSeen && i != len(parts)-1 {
			return false
		}

		// + and # cannot be mixed with other characters in the same level
		if strings.Contains(part, "+") && part != "+" {
			return false
		}
		if strings.Contains(part, "#") && part != "#" {
			return false
		}
	}

	return true
}

// NormalizeTopic normalizes an MQTT topic by cleaning up the path
func NormalizeTopic(topic string) string {
	// Use path.Clean to normalize, but preserve trailing slash if present
	hasTrailingSlash := strings.HasSuffix(topic, "/")
	normalized := path.Clean(topic)

	// path.Clean removes trailing slashes, restore if needed
	if hasTrailingSlash && !strings.HasSuffix(normalized, "/") {
		normalized += "/"
	}

	return normalized
}
