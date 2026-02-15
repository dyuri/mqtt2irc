package config

import (
	"fmt"
	"strings"
)

// Validate checks if the configuration is valid
func Validate(cfg *Config) error {
	// MQTT validation
	if cfg.MQTT.Broker == "" {
		return fmt.Errorf("mqtt.broker is required")
	}
	if cfg.MQTT.ClientID == "" {
		return fmt.Errorf("mqtt.client_id is required")
	}
	if cfg.MQTT.QoS > 2 {
		return fmt.Errorf("mqtt.qos must be 0, 1, or 2")
	}
	if len(cfg.MQTT.Topics) == 0 {
		return fmt.Errorf("mqtt.topics must have at least one topic")
	}
	for i, topic := range cfg.MQTT.Topics {
		if topic.Pattern == "" {
			return fmt.Errorf("mqtt.topics[%d].pattern is required", i)
		}
		if topic.QoS > 2 {
			return fmt.Errorf("mqtt.topics[%d].qos must be 0, 1, or 2", i)
		}
	}

	// IRC validation
	if cfg.IRC.Server == "" {
		return fmt.Errorf("irc.server is required")
	}
	if cfg.IRC.Nickname == "" {
		return fmt.Errorf("irc.nickname is required")
	}
	if cfg.IRC.RateLimit.MessagesPerSecond <= 0 {
		return fmt.Errorf("irc.rate_limit.messages_per_second must be positive")
	}
	if cfg.IRC.RateLimit.Burst <= 0 {
		return fmt.Errorf("irc.rate_limit.burst must be positive")
	}

	// Bridge validation
	if len(cfg.Bridge.Mappings) == 0 {
		return fmt.Errorf("bridge.mappings must have at least one mapping")
	}
	for i, mapping := range cfg.Bridge.Mappings {
		if mapping.MQTTTopic == "" {
			return fmt.Errorf("bridge.mappings[%d].mqtt_topic is required", i)
		}
		if len(mapping.IRCChannels) == 0 {
			return fmt.Errorf("bridge.mappings[%d].irc_channels must have at least one channel", i)
		}
		for j, channel := range mapping.IRCChannels {
			if !strings.HasPrefix(channel, "#") && !strings.HasPrefix(channel, "&") {
				return fmt.Errorf("bridge.mappings[%d].irc_channels[%d] must start with # or &", i, j)
			}
		}
	}
	if cfg.Bridge.Queue.MaxSize <= 0 {
		return fmt.Errorf("bridge.queue.max_size must be positive")
	}
	if cfg.Bridge.MaxMessageLength <= 0 {
		return fmt.Errorf("bridge.max_message_length must be positive")
	}

	// Logging validation
	validLevels := map[string]bool{"trace": true, "debug": true, "info": true, "warn": true, "error": true, "fatal": true, "panic": true}
	if !validLevels[cfg.Logging.Level] {
		return fmt.Errorf("logging.level must be one of: trace, debug, info, warn, error, fatal, panic")
	}

	// Health validation
	if cfg.Health.Enabled && (cfg.Health.Port <= 0 || cfg.Health.Port > 65535) {
		return fmt.Errorf("health.port must be between 1 and 65535")
	}

	return nil
}
