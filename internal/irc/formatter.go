package irc

import (
	"bytes"
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

	// Parse template
	tmpl, err := template.New("message").Parse(templateStr)
	if err != nil {
		// Fallback to simple format if template is invalid
		return formatSimple(msg, maxLength, truncateSuffix), nil
	}

	// Template data
	data := map[string]interface{}{
		"Topic":   msg.Topic,
		"Payload": string(msg.Payload),
		"QoS":     msg.QoS,
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

// formatSimple creates a simple formatted message
func formatSimple(msg types.Message, maxLength int, truncateSuffix string) string {
	result := "[" + msg.Topic + "] " + string(msg.Payload)
	result = sanitize(result)
	result = truncate(result, maxLength, truncateSuffix)
	return result
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
