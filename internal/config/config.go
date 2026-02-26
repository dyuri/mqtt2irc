package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	MQTT    MQTTConfig    `mapstructure:"mqtt"`
	IRC     IRCConfig     `mapstructure:"irc"`
	Bridge  BridgeConfig  `mapstructure:"bridge"`
	Logging LoggingConfig `mapstructure:"logging"`
	Health  HealthConfig  `mapstructure:"health"`
	Admin   AdminConfig   `mapstructure:"admin"`
}

// AdminConfig contains IRC admin command system configuration
type AdminConfig struct {
	Enabled       bool             `mapstructure:"enabled"`
	CommandPrefix string           `mapstructure:"command_prefix"`
	AllowList     []AdminAllowEntry `mapstructure:"allow_list"`
	Channels      []string         `mapstructure:"channels"`
	AcceptPM      bool             `mapstructure:"accept_pm"`
}

// AdminAllowEntry defines an authorized IRC user for admin commands
type AdminAllowEntry struct {
	Nick     string `mapstructure:"nick"`
	Hostmask string `mapstructure:"hostmask"`
}

// MQTTConfig contains MQTT broker configuration
type MQTTConfig struct {
	Broker   string        `mapstructure:"broker"`
	ClientID string        `mapstructure:"client_id"`
	Username string        `mapstructure:"username"`
	Password string        `mapstructure:"password"`
	QoS      byte          `mapstructure:"qos"`
	Topics   []TopicConfig `mapstructure:"topics"`
	UseTLS   bool          `mapstructure:"use_tls"`
}

// TopicConfig represents an MQTT topic subscription
type TopicConfig struct {
	Pattern string `mapstructure:"pattern"`
	QoS     byte   `mapstructure:"qos"`
}

// IRCConfig contains IRC server configuration
type IRCConfig struct {
	Server           string         `mapstructure:"server"`
	UseTLS           bool           `mapstructure:"use_tls"`
	Nickname         string         `mapstructure:"nickname"`
	Username         string         `mapstructure:"username"`
	Realname         string         `mapstructure:"realname"`
	NickServPassword string         `mapstructure:"nickserv_password"`
	RateLimit        RateLimitConfig `mapstructure:"rate_limit"`
}

// RateLimitConfig contains IRC rate limiting settings
type RateLimitConfig struct {
	MessagesPerSecond float64 `mapstructure:"messages_per_second"`
	Burst             int     `mapstructure:"burst"`
}

// BridgeConfig contains bridge behavior configuration
type BridgeConfig struct {
	Mappings         []MappingConfig `mapstructure:"mappings"`
	Queue            QueueConfig     `mapstructure:"queue"`
	MaxMessageLength int             `mapstructure:"max_message_length"`
	TruncateSuffix   string          `mapstructure:"truncate_suffix"`
}

// MappingConfig maps MQTT topics to IRC channels
type MappingConfig struct {
	MQTTTopic       string                 `mapstructure:"mqtt_topic"`
	IRCChannels     []string               `mapstructure:"irc_channels"`
	MessageFormat   string                 `mapstructure:"message_format"`
	Processor       string                 `mapstructure:"processor"`
	ProcessorConfig map[string]interface{} `mapstructure:"processor_config"`
}

// QueueConfig contains message queue settings
type QueueConfig struct {
	MaxSize     int  `mapstructure:"max_size"`
	BlockOnFull bool `mapstructure:"block_on_full"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// HealthConfig contains health check server settings
type HealthConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`
}

// Load reads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("mqtt.qos", 1)
	v.SetDefault("mqtt.use_tls", true)
	v.SetDefault("irc.use_tls", true)
	v.SetDefault("irc.rate_limit.messages_per_second", 2.0)
	v.SetDefault("irc.rate_limit.burst", 5)
	v.SetDefault("bridge.queue.max_size", 1000)
	v.SetDefault("bridge.queue.block_on_full", false)
	v.SetDefault("bridge.max_message_length", 400)
	v.SetDefault("bridge.truncate_suffix", "...")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("health.enabled", true)
	v.SetDefault("health.port", 8080)
	v.SetDefault("admin.enabled", false)
	v.SetDefault("admin.command_prefix", "!")
	v.SetDefault("admin.accept_pm", true)

	// Configure Viper
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")
	}

	// Environment variable support
	v.SetEnvPrefix("MQTT2IRC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}
