package irc

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lrstanley/girc"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/dyuri/mqtt2irc/internal/config"
)

// Client wraps the IRC client
type Client struct {
	client      *girc.Client
	config      config.IRCConfig
	logger      zerolog.Logger
	limiter     *rate.Limiter
	channels    map[string]bool
	mu          sync.RWMutex
	ready       chan struct{}
	readyClosed bool
}

// New creates a new IRC client
func New(cfg config.IRCConfig, logger zerolog.Logger) *Client {
	c := &Client{
		config:   cfg,
		logger:   logger.With().Str("component", "irc").Logger(),
		channels: make(map[string]bool),
		ready:    make(chan struct{}),
	}

	// Create rate limiter (token bucket)
	c.limiter = rate.NewLimiter(
		rate.Limit(cfg.RateLimit.MessagesPerSecond),
		cfg.RateLimit.Burst,
	)

	// Configure girc client
	ircCfg := girc.Config{
		Server: cfg.Server,
		Port:   6667, // Default port
		Nick:   cfg.Nickname,
		User:   cfg.Username,
		Name:   cfg.Realname,
	}

	// Parse server and port if provided in "host:port" format
	if strings.Contains(cfg.Server, ":") {
		parts := strings.Split(cfg.Server, ":")
		ircCfg.Server = parts[0]
		if len(parts) > 1 {
			// Parse port from string
			if port, err := strconv.Atoi(parts[1]); err == nil {
				ircCfg.Port = port
			}
		}
	}

	// TLS configuration
	if cfg.UseTLS {
		ircCfg.SSL = true
		ircCfg.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	c.client = girc.New(ircCfg)

	// Set up event handlers
	c.client.Handlers.Add(girc.CONNECTED, c.onConnect)
	c.client.Handlers.Add(girc.DISCONNECTED, c.onDisconnect)
	c.client.Handlers.Add(girc.JOIN, c.onJoin)

	return c
}

// Connect establishes connection to IRC server
func (c *Client) Connect(ctx context.Context) error {
	c.logger.Info().Str("server", c.config.Server).Msg("connecting to IRC server")

	// Connect in background
	errChan := make(chan error, 1)
	go func() {
		if err := c.client.Connect(); err != nil {
			c.logger.Error().Err(err).Msg("IRC connect error")
			errChan <- err
		}
	}()

	// Wait for connection with a reasonable timeout
	timeout := time.After(30 * time.Second)

	// Wait for connection or context cancellation
	select {
	case err := <-errChan:
		return fmt.Errorf("failed to connect to IRC server: %w", err)
	case <-c.ready:
		c.logger.Info().Msg("connected to IRC server")
		return nil
	case <-timeout:
		c.client.Close()
		return fmt.Errorf("IRC connection timeout")
	case <-ctx.Done():
		c.client.Close()
		return ctx.Err()
	}
}

// onConnect is called when connection is established
func (c *Client) onConnect(client *girc.Client, event girc.Event) {
	c.logger.Info().Msg("IRC connection established")

	// Authenticate with NickServ if configured
	if c.config.NickServPassword != "" {
		c.logger.Info().Msg("authenticating with NickServ")
		c.client.Cmd.Message("NickServ", fmt.Sprintf("IDENTIFY %s", c.config.NickServPassword))
		// Give NickServ time to process
		time.Sleep(2 * time.Second)
	}

	// Signal that we're ready (guard against double-close on reconnect cycles)
	c.mu.Lock()
	if !c.readyClosed {
		close(c.ready)
		c.readyClosed = true
	}
	c.mu.Unlock()
}

// onDisconnect is called when connection is lost
func (c *Client) onDisconnect(client *girc.Client, event girc.Event) {
	c.logger.Warn().Msg("IRC connection lost")
}

// onJoin is called when we join a channel
func (c *Client) onJoin(client *girc.Client, event girc.Event) {
	if event.Source.Name == c.client.GetNick() {
		channel := event.Params[0]
		c.mu.Lock()
		c.channels[channel] = true
		c.mu.Unlock()
		c.logger.Info().Str("channel", channel).Msg("joined IRC channel")
	}
}

// JoinChannel joins an IRC channel
func (c *Client) JoinChannel(channel string) {
	c.mu.RLock()
	alreadyJoined := c.channels[channel]
	c.mu.RUnlock()

	if !alreadyJoined {
		c.logger.Info().Str("channel", channel).Msg("joining IRC channel")
		c.client.Cmd.Join(channel)
	}
}

// SendMessage sends a message to an IRC channel with rate limiting
func (c *Client) SendMessage(ctx context.Context, channel, message string) error {
	// Ensure we're in the channel
	c.JoinChannel(channel)

	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}

	// Send message
	c.logger.Debug().
		Str("channel", channel).
		Str("message", message).
		Msg("sending message to IRC")

	c.client.Cmd.Message(channel, message)
	return nil
}

// Disconnect closes the IRC connection
func (c *Client) Disconnect() {
	c.logger.Info().Msg("disconnecting from IRC server")
	c.client.Close()
	c.logger.Info().Msg("disconnected from IRC server")
}

// IsConnected returns true if connected to IRC server
func (c *Client) IsConnected() bool {
	return c.client.IsConnected()
}

// Nick changes the bot's IRC nickname.
func (c *Client) Nick(newnick string) {
	c.client.Cmd.Nick(newnick)
}

// Reconnect drops the current connection and reconnects.
// girc v1.1.1 has no built-in Reconnect(); we reset state and re-call Connect().
func (c *Client) Reconnect() {
	c.mu.Lock()
	c.ready = make(chan struct{})
	c.readyClosed = false
	c.channels = make(map[string]bool)
	c.mu.Unlock()
	c.client.Close()
	go func() {
		if err := c.client.Connect(); err != nil {
			c.logger.Error().Err(err).Msg("IRC reconnect failed")
		}
	}()
}

// AddHandler registers an additional girc event handler.
func (c *Client) AddHandler(event string, handler func(*girc.Client, girc.Event)) {
	c.client.Handlers.Add(event, handler)
}
