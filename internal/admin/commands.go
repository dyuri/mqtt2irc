package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/lrstanley/girc"
)

// dispatch parses the command text and calls the appropriate handler.
func (h *Handler) dispatch(client *girc.Client, replyTo, text string) {
	// Strip prefix and split into command + args.
	withoutPrefix := strings.TrimPrefix(text, h.cfg.CommandPrefix)
	parts := strings.Fields(withoutPrefix)
	if len(parts) == 0 {
		return
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "help":
		h.cmdHelp(client, replyTo)
	case "status", "health":
		h.cmdStatus(client, replyTo)
	case "nick":
		h.cmdNick(client, replyTo, args)
	case "reconnect":
		h.cmdReconnect(client, replyTo, args)
	case "shutdown":
		h.cmdShutdown(client, replyTo)
	default:
		h.reply(client, replyTo, fmt.Sprintf("Unknown command: %s%s — try %shelp", h.cfg.CommandPrefix, cmd, h.cfg.CommandPrefix))
	}
}

func (h *Handler) cmdHelp(client *girc.Client, replyTo string) {
	p := h.cfg.CommandPrefix
	lines := []string{
		fmt.Sprintf("Admin commands (prefix: %s):", p),
		fmt.Sprintf("  %shelp                — show this help", p),
		fmt.Sprintf("  %sstatus / %shealth    — show bridge connection status", p, p),
		fmt.Sprintf("  %snick <newnick>      — change bot IRC nickname", p),
		fmt.Sprintf("  %sreconnect mqtt      — reconnect to MQTT broker", p),
		fmt.Sprintf("  %sreconnect irc       — reconnect to IRC server", p),
		fmt.Sprintf("  %sshutdown            — gracefully shut down the bridge", p),
	}
	for _, line := range lines {
		h.reply(client, replyTo, line)
	}
}

func (h *Handler) cmdStatus(client *girc.Client, replyTo string) {
	status := h.bridge.HealthStatus()
	mqttOK, _ := status["mqtt_connected"].(bool)
	ircOK, _ := status["irc_connected"].(bool)
	queueSize, _ := status["queue_size"].(int)
	queueCap, _ := status["queue_capacity"].(int)

	mqttStr := "connected"
	if !mqttOK {
		mqttStr = "DISCONNECTED"
	}
	ircStr := "connected"
	if !ircOK {
		ircStr = "DISCONNECTED"
	}

	h.reply(client, replyTo, fmt.Sprintf(
		"Bridge status: MQTT=%s IRC=%s queue=%d/%d",
		mqttStr, ircStr, queueSize, queueCap,
	))
}

func (h *Handler) cmdNick(client *girc.Client, replyTo string, args []string) {
	if len(args) == 0 {
		h.reply(client, replyTo, "Usage: !nick <newnick>")
		return
	}
	newnick := args[0]
	if len(newnick) > 30 {
		h.reply(client, replyTo, "Nick too long (max 30 characters)")
		return
	}
	if strings.ContainsAny(newnick, " \t\r\n") {
		h.reply(client, replyTo, "Invalid nick: must not contain whitespace")
		return
	}
	h.logger.Info().Str("newnick", newnick).Msg("admin nick change")
	h.bridge.NickChange(newnick)
	h.reply(client, replyTo, fmt.Sprintf("Changing nick to: %s", newnick))
}

func (h *Handler) cmdReconnect(client *girc.Client, replyTo string, args []string) {
	if len(args) == 0 {
		h.reply(client, replyTo, "Usage: !reconnect <mqtt|irc>")
		return
	}
	switch strings.ToLower(args[0]) {
	case "mqtt":
		h.logger.Info().Msg("admin MQTT reconnect")
		h.reply(client, replyTo, "Reconnecting to MQTT broker...")
		h.bridge.ReconnectMQTT()
	case "irc":
		h.logger.Info().Msg("admin IRC reconnect")
		h.reply(client, replyTo, "Reconnecting to IRC server...")
		h.bridge.ReconnectIRC()
	default:
		h.reply(client, replyTo, fmt.Sprintf("Unknown target: %s (use 'mqtt' or 'irc')", args[0]))
	}
}

func (h *Handler) cmdShutdown(client *girc.Client, replyTo string) {
	h.logger.Warn().Msg("admin shutdown command received")
	h.reply(client, replyTo, "Shutting down...")
	// Send in background so the reply can be delivered before we shutdown.
	ctx := context.Background()
	go func() {
		// Re-send via bridge.SendMessage so it goes through the rate limiter.
		_ = h.bridge.SendMessage(ctx, replyTo, "Goodbye.")
		h.shutdownFn()
	}()
}
