package irc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/dyuri/mqtt2irc/pkg/types"
)

// FormatMessage formats an MQTT message for IRC using a template
func FormatMessage(msg types.Message, templateStr string, maxLength int, truncateSuffix string) (string, error) {
	// Default template if none provided
	if templateStr == "" {
		templateStr = "[{{.Topic}}] {{.Payload}}"
	}

	// Parse template; missingkey=zero returns "" for missing JSON fields (string zero value)
	tmpl, err := template.New("message").Option("missingkey=zero").Parse(templateStr)
	if err != nil {
		// Fallback to simple format if template is invalid
		return formatSimple(msg, maxLength, truncateSuffix), nil
	}

	// Template data
	data := map[string]interface{}{
		"Topic":   msg.Topic,
		"Payload": payloadString(msg.Payload),
		"QoS":     msg.QoS,
		"JSON":    ParseJSON(msg.Payload),
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// Fallback to simple format if execution fails
		return formatSimple(msg, maxLength, truncateSuffix), nil
	}

	result := buf.String()

	// Sanitize and truncate
	result = sanitize(result)
	result = truncate(result, maxLength, truncateSuffix)

	return result, nil
}

// ParseJSON attempts to parse a payload as a JSON object.
// Returns a map[string]string on success (values are stringified), nil otherwise.
// Only JSON objects (not arrays or scalars) are supported.
// Using string values ensures missing keys produce "" in templates rather than "<no value>".
func ParseJSON(payload []byte) map[string]string {
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

// payloadString converts a payload to a display string.
// If the payload is not valid UTF-8 (i.e. binary), returns a descriptive placeholder.
func payloadString(payload []byte) string {
	if !utf8.Valid(payload) {
		return fmt.Sprintf("[binary data, %d bytes]", len(payload))
	}
	return string(payload)
}

// formatSimple creates a simple formatted message
func formatSimple(msg types.Message, maxLength int, truncateSuffix string) string {
	result := "[" + msg.Topic + "] " + payloadString(msg.Payload)
	result = sanitize(result)
	result = truncate(result, maxLength, truncateSuffix)
	return result
}

// SanitizeAndTruncate applies IRC sanitization and length truncation to a pre-formatted string.
// This is the exported entry point for message processors that pre-format their output.
func SanitizeAndTruncate(s string, maxLen int, suffix string) string {
	s = sanitize(s)
	s = truncate(s, maxLen, suffix)
	return s
}

// sanitize removes or replaces problematic characters for IRC
func sanitize(s string) string {
	// Remove control characters except for common formatting codes
	var result strings.Builder
	for _, r := range s {
		// Allow printable characters and IRC color codes
		if r >= 32 && r < 127 || r == '\x02' || r == '\x1F' || r == '\x16' || r == '\x03' {
			result.WriteRune(r)
		} else if r >= 128 { // Allow UTF-8
			result.WriteRune(r)
		} else {
			// Replace control chars with space
			result.WriteRune(' ')
		}
	}

	// Collapse multiple spaces
	s = result.String()
	s = strings.Join(strings.Fields(s), " ")

	return s
}

// truncate limits message length for IRC protocol
func truncate(s string, maxLength int, suffix string) string {
	if maxLength <= 0 {
		maxLength = 400
	}

	if utf8.RuneCountInString(s) <= maxLength {
		return s
	}

	// Reserve space for suffix
	targetLen := maxLength - utf8.RuneCountInString(suffix)
	if targetLen <= 0 {
		return suffix
	}

	// Truncate to rune boundary
	runes := []rune(s)
	return string(runes[:targetLen]) + suffix
}
