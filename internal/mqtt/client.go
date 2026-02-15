package mqtt

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"

	"github.com/dyuri/mqtt2irc/internal/config"
	"github.com/dyuri/mqtt2irc/pkg/types"
)

// Client wraps the MQTT client
type Client struct {
	client  pahomqtt.Client
	config  config.MQTTConfig
	msgChan chan<- types.Message
	logger  zerolog.Logger
}

// New creates a new MQTT client
func New(cfg config.MQTTConfig, msgChan chan<- types.Message, logger zerolog.Logger) (*Client, error) {
	c := &Client{
		config:  cfg,
		msgChan: msgChan,
		logger:  logger.With().Str("component", "mqtt").Logger(),
	}

	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	if cfg.UseTLS {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		opts.SetTLSConfig(tlsConfig)
	}

	// Connection handlers
	opts.SetOnConnectHandler(c.onConnect)
	opts.SetConnectionLostHandler(c.onConnectionLost)
	opts.SetReconnectingHandler(c.onReconnecting)

	// Reconnection settings
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(60 * time.Second)
	opts.SetConnectRetryInterval(1 * time.Second)
	opts.SetConnectRetry(true)

	// Keep alive
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	// Clean session
	opts.SetCleanSession(true)

	c.client = pahomqtt.NewClient(opts)

	return c, nil
}

// Connect establishes connection to MQTT broker
func (c *Client) Connect(ctx context.Context) error {
	c.logger.Info().Str("broker", c.config.Broker).Msg("connecting to MQTT broker")

	token := c.client.Connect()

	// Wait for connection with context
	select {
	case <-token.Done():
		if token.Error() != nil {
			return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	c.logger.Info().Msg("connected to MQTT broker")
	return nil
}

// onConnect is called when connection is established
func (c *Client) onConnect(client pahomqtt.Client) {
	c.logger.Info().Msg("MQTT connection established")

	// Subscribe to all configured topics
	for _, topic := range c.config.Topics {
		c.logger.Info().
			Str("pattern", topic.Pattern).
			Uint8("qos", topic.QoS).
			Msg("subscribing to MQTT topic")

		token := client.Subscribe(topic.Pattern, topic.QoS, c.messageHandler)
		if token.Wait() && token.Error() != nil {
			c.logger.Error().
				Err(token.Error()).
				Str("pattern", topic.Pattern).
				Msg("failed to subscribe to topic")
		} else {
			c.logger.Info().
				Str("pattern", topic.Pattern).
				Msg("subscribed to topic")
		}
	}
}

// onConnectionLost is called when connection is lost
func (c *Client) onConnectionLost(client pahomqtt.Client, err error) {
	c.logger.Warn().Err(err).Msg("MQTT connection lost")
}

// onReconnecting is called when attempting to reconnect
func (c *Client) onReconnecting(client pahomqtt.Client, opts *pahomqtt.ClientOptions) {
	c.logger.Info().Msg("attempting to reconnect to MQTT broker")
}

// messageHandler processes incoming MQTT messages
func (c *Client) messageHandler(client pahomqtt.Client, msg pahomqtt.Message) {
	message := types.Message{
		Topic:     msg.Topic(),
		Payload:   msg.Payload(),
		Timestamp: time.Now(),
		QoS:       msg.Qos(),
	}

	c.logger.Debug().
		Str("topic", message.Topic).
		Int("payload_size", len(message.Payload)).
		Msg("received MQTT message")

	// Send to bridge (non-blocking if channel is full)
	select {
	case c.msgChan <- message:
		// Message sent successfully
	default:
		c.logger.Warn().
			Str("topic", message.Topic).
			Msg("message queue full, dropping message")
	}
}

// Disconnect closes the MQTT connection
func (c *Client) Disconnect(timeout time.Duration) {
	c.logger.Info().Msg("disconnecting from MQTT broker")
	c.client.Disconnect(uint(timeout.Milliseconds()))
	c.logger.Info().Msg("disconnected from MQTT broker")
}

// IsConnected returns true if connected to MQTT broker
func (c *Client) IsConnected() bool {
	return c.client.IsConnected()
}
