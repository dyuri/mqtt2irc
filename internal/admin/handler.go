// Package admin provides IRC-based admin command handling for the mqtt2irc bridge.
package admin

import (
	"context"
	"path"
	"strings"

	"github.com/lrstanley/girc"
	"github.com/rs/zerolog"
)

// BridgeAdmin is the interface the Bridge must satisfy for admin commands.
// Defined here to avoid circular imports (admin does not import bridge).
type BridgeAdmin interface {
	HealthStatus() map[string]interface{}
	SendMessage(ctx context.Context, channel, message string) error
	NickChange(newnick string)
	ReconnectIRC()
	ReconnectMQTT()
}

// AllowEntry defines an authorized IRC user for admin commands.
type AllowEntry struct {
	Nick     string // case-insensitive match
	Hostmask string // optional glob, e.g. "*@trusted.net" (uses path.Match)
}

// Config holds the admin command handler configuration.
type Config struct {
	Enabled       bool
	CommandPrefix string
	AllowList     []AllowEntry
	Channels      []string // IRC channels where commands are accepted
	AcceptPM      bool     // also accept commands via private message
}

// Handler processes incoming IRC PRIVMSG events and dispatches admin commands.
type Handler struct {
	cfg        Config
	bridge     BridgeAdmin
	shutdownFn func()
	logger     zerolog.Logger
}

// New creates a new admin Handler.
func New(cfg Config, bridge BridgeAdmin, shutdownFn func(), logger zerolog.Logger) *Handler {
	if cfg.CommandPrefix == "" {
		cfg.CommandPrefix = "!"
	}
	return &Handler{
		cfg:        cfg,
		bridge:     bridge,
		shutdownFn: shutdownFn,
		logger:     logger.With().Str("component", "admin").Logger(),
	}
}

// GircHandler returns a girc PRIVMSG handler function suitable for registration
// via client.Handlers.Add(girc.PRIVMSG, ...).
func (h *Handler) GircHandler() func(*girc.Client, girc.Event) {
	return h.onPRIVMSG
}

// onPRIVMSG is called for every incoming PRIVMSG event.
func (h *Handler) onPRIVMSG(client *girc.Client, event girc.Event) {
	if len(event.Params) == 0 || event.Source == nil {
		return
	}

	target := event.Params[0]      // channel or bot nick
	text := event.Last()           // message text
	senderNick := event.Source.Name
	senderHost := event.Source.Ident + "@" + event.Source.Host

	botNick := client.GetNick()
	isPM := strings.EqualFold(target, botNick)

	// Determine if this message comes from an accepted source.
	if !h.acceptsSource(target, isPM) {
		return
	}

	// Only handle messages that start with the command prefix.
	if !strings.HasPrefix(text, h.cfg.CommandPrefix) {
		return
	}

	// Audit log every command attempt.
	h.logger.Info().
		Str("nick", senderNick).
		Str("host", senderHost).
		Str("target", target).
		Str("text", text).
		Msg("admin command attempt")

	// Authorize sender.
	if !h.isAuthorized(senderNick, senderHost) {
		h.logger.Warn().
			Str("nick", senderNick).
			Str("host", senderHost).
			Msg("unauthorized admin command attempt")
		return
	}

	// Determine reply target: if PM, reply to sender; otherwise reply to channel.
	replyTo := target
	if isPM {
		replyTo = senderNick
	}

	h.dispatch(client, replyTo, text)
}

// acceptsSource reports whether the given message target is an accepted source.
func (h *Handler) acceptsSource(target string, isPM bool) bool {
	if isPM {
		return h.cfg.AcceptPM
	}
	// Check if target channel is in the allowed channels list.
	for _, ch := range h.cfg.Channels {
		if strings.EqualFold(ch, target) {
			return true
		}
	}
	return false
}

// isAuthorized reports whether the given nick+hostmask is allowed to run commands.
func (h *Handler) isAuthorized(nick, hostmask string) bool {
	for _, entry := range h.cfg.AllowList {
		if !strings.EqualFold(entry.Nick, nick) {
			continue
		}
		if entry.Hostmask == "" {
			return true
		}
		matched, err := path.Match(entry.Hostmask, hostmask)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// reply sends a PRIVMSG reply to the given target.
func (h *Handler) reply(client *girc.Client, target, message string) {
	client.Cmd.Message(target, message)
}
