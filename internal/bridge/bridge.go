package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lrstanley/girc"
	"github.com/rs/zerolog"

	"github.com/dyuri/mqtt2irc/internal/config"
	"github.com/dyuri/mqtt2irc/internal/irc"
	"github.com/dyuri/mqtt2irc/internal/mqtt"
	"github.com/dyuri/mqtt2irc/pkg/types"
)

// Bridge coordinates message flow from MQTT to IRC
type Bridge struct {
	config     config.BridgeConfig
	mqttClient *mqtt.Client
	ircClient  *irc.Client
	mapper     *Mapper
	processors map[string]Processor // mqtt_topic pattern → Processor (nil if none configured)
	msgQueue   chan types.Message
	logger     zerolog.Logger
	wg         sync.WaitGroup
}

// New creates a new bridge instance
func New(cfg *config.Config, logger zerolog.Logger) (*Bridge, error) {
	// Create message queue
	msgQueue := make(chan types.Message, cfg.Bridge.Queue.MaxSize)

	// Create MQTT client
	mqttClient, err := mqtt.New(cfg.MQTT, msgQueue, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create MQTT client: %w", err)
	}

	// Create IRC client
	ircClient := irc.New(cfg.IRC, logger)

	// Create mapper
	mapper := NewMapper(cfg.Bridge.Mappings)

	// Instantiate processors for mappings that declare one.
	processors := make(map[string]Processor)
	for _, m := range cfg.Bridge.Mappings {
		if m.Processor == "" {
			continue
		}
		p, err := NewProcessor(m.Processor, m.ProcessorConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create processor for mapping %q: %w", m.MQTTTopic, err)
		}
		processors[m.MQTTTopic] = p
	}

	return &Bridge{
		config:     cfg.Bridge,
		mqttClient: mqttClient,
		ircClient:  ircClient,
		mapper:     mapper,
		processors: processors,
		msgQueue:   msgQueue,
		logger:     logger.With().Str("component", "bridge").Logger(),
	}, nil
}

// Run starts the bridge
func (b *Bridge) Run(ctx context.Context) error {
	b.logger.Info().Msg("starting bridge")

	// Connect to MQTT
	if err := b.mqttClient.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// Connect to IRC
	if err := b.ircClient.Connect(ctx); err != nil {
		b.mqttClient.Disconnect(5 * time.Second)
		return fmt.Errorf("failed to connect to IRC: %w", err)
	}

	// Start message processor
	b.wg.Add(1)
	go b.processMessages(ctx)

	b.logger.Info().Msg("bridge running")

	// Wait for context cancellation
	<-ctx.Done()
	b.logger.Info().Msg("bridge shutting down")

	return nil
}

// processMessages processes messages from the queue
func (b *Bridge) processMessages(ctx context.Context) {
	defer b.wg.Done()

	for {
		select {
		case <-ctx.Done():
			b.logger.Info().Msg("stopping message processor")
			return

		case msg := <-b.msgQueue:
			b.handleMessage(ctx, msg)
		}
	}
}

// handleMessage processes a single message
func (b *Bridge) handleMessage(ctx context.Context, msg types.Message) {
	// Find matching mappings
	mappings := b.mapper.Map(msg.Topic)

	if len(mappings) == 0 {
		b.logger.Debug().
			Str("topic", msg.Topic).
			Msg("no mapping found for topic")
		return
	}

	b.logger.Debug().
		Str("topic", msg.Topic).
		Int("mappings", len(mappings)).
		Msg("processing message")

	// Debug: log payload and JSON parsing result
	if b.logger.GetLevel() <= zerolog.DebugLevel {
		jsonData := irc.ParseJSON(msg.Payload)
		ev := b.logger.Debug().
			Str("topic", msg.Topic).
			Str("payload", string(msg.Payload))
		if jsonData == nil {
			ev.Bool("json_parsed", false)
		} else {
			keys := make([]string, 0, len(jsonData))
			for k := range jsonData {
				keys = append(keys, k)
			}
			ev.Bool("json_parsed", true).Strs("json_keys", keys)
		}
		ev.Msg("message payload")
	}

	// Send to all matched channels
	for _, mapping := range mappings {
		var formatted string

		// If a processor is registered for this mapping, run it first.
		if proc, ok := b.processors[mapping.MQTTTopic]; ok {
			result, err := proc.Process(msg)
			if err != nil {
				b.logger.Error().
					Err(err).
					Str("topic", msg.Topic).
					Str("processor", mapping.Processor).
					Msg("processor error")
			}
			if result.Drop {
				b.logger.Debug().
					Str("topic", msg.Topic).
					Msg("message dropped by processor")
				continue
			}
			if result.Formatted != "" {
				formatted = irc.SanitizeAndTruncate(
					result.Formatted,
					b.config.MaxMessageLength,
					b.config.TruncateSuffix,
				)
				// Send pre-formatted output directly, skipping FormatMessage.
				for _, channel := range mapping.IRCChannels {
					if err := b.ircClient.SendMessage(ctx, channel, formatted); err != nil {
						b.logger.Error().
							Err(err).
							Str("channel", channel).
							Str("topic", msg.Topic).
							Msg("failed to send message to IRC")
					} else {
						b.logger.Debug().
							Str("channel", channel).
							Str("topic", msg.Topic).
							Msg("message sent to IRC")
					}
				}
				continue
			}
		}

		// No processor, or processor passed through — use normal template formatting.
		var err error
		formatted, err = irc.FormatMessage(
			msg,
			mapping.MessageFormat,
			b.config.MaxMessageLength,
			b.config.TruncateSuffix,
		)
		if err != nil {
			b.logger.Error().
				Err(err).
				Str("topic", msg.Topic).
				Msg("failed to format message")
			continue
		}

		// Send to each IRC channel
		for _, channel := range mapping.IRCChannels {
			if err := b.ircClient.SendMessage(ctx, channel, formatted); err != nil {
				b.logger.Error().
					Err(err).
					Str("channel", channel).
					Str("topic", msg.Topic).
					Msg("failed to send message to IRC")
			} else {
				b.logger.Debug().
					Str("channel", channel).
					Str("topic", msg.Topic).
					Msg("message sent to IRC")
			}
		}
	}
}

// Shutdown gracefully shuts down the bridge
func (b *Bridge) Shutdown(ctx context.Context) error {
	b.logger.Info().Msg("shutting down bridge")

	// Close message queue (no new messages)
	close(b.msgQueue)

	// Wait for message processor to finish with timeout
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		b.logger.Info().Msg("message processor stopped")
	case <-ctx.Done():
		b.logger.Warn().Msg("shutdown timeout, forcing stop")
	}

	// Disconnect clients
	b.mqttClient.Disconnect(5 * time.Second)
	b.ircClient.Disconnect()

	b.logger.Info().Msg("bridge shutdown complete")
	return nil
}

// HealthStatus returns the health status of the bridge
func (b *Bridge) HealthStatus() map[string]interface{} {
	return map[string]interface{}{
		"mqtt_connected": b.mqttClient.IsConnected(),
		"irc_connected":  b.ircClient.IsConnected(),
		"queue_size":     len(b.msgQueue),
		"queue_capacity": cap(b.msgQueue),
	}
}

// SendMessage sends a message to an IRC channel (implements admin.BridgeAdmin).
func (b *Bridge) SendMessage(ctx context.Context, channel, message string) error {
	return b.ircClient.SendMessage(ctx, channel, message)
}

// NickChange changes the bot's IRC nickname (implements admin.BridgeAdmin).
func (b *Bridge) NickChange(newnick string) {
	b.ircClient.Nick(newnick)
}

// ReconnectIRC drops and re-establishes the IRC connection (implements admin.BridgeAdmin).
func (b *Bridge) ReconnectIRC() {
	b.ircClient.Reconnect()
}

// ReconnectMQTT drops and re-establishes the MQTT connection (implements admin.BridgeAdmin).
func (b *Bridge) ReconnectMQTT() {
	b.mqttClient.ForceReconnect()
}

// AddIRCHandler registers an additional girc event handler.
func (b *Bridge) AddIRCHandler(event string, handler func(*girc.Client, girc.Event)) {
	b.ircClient.AddHandler(event, handler)
}
