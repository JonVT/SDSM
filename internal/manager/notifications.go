package manager

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"sdsm/internal/integrations/discord"
	"sdsm/internal/models"
)

// DiscordNotify posts a simple embed or content message to the manager's default webhook.
// It is best-effort; errors are logged to the manager log and otherwise ignored.
func (m *Manager) DiscordNotify(content string, embeds ...discord.Embed) {
	if m == nil {
		return
	}
	m.DiscordNotifyTo(strings.TrimSpace(m.DiscordDefaultWebhook), content, embeds...)
}

// DiscordNotifyTo posts a message to the specified webhook. Best-effort.
func (m *Manager) DiscordNotifyTo(webhook, content string, embeds ...discord.Embed) {
	if m == nil || m.Log == nil {
		return
	}
	wh := strings.TrimSpace(webhook)
	if wh == "" {
		return
	}
	payload := discord.WebhookPayload{Content: strings.TrimSpace(content)}
	if len(embeds) > 0 {
		payload.Embeds = embeds
	}
	status, err := discord.Post(wh, payload)
	if err != nil || status < 200 || status >= 300 {
		m.Log.Write(fmt.Sprintf("Discord notify failed (status=%d): %v", status, err))
	}
}

func (m *Manager) notifyDeployStart(dt DeployType) {
	if m == nil || !m.NotifyEnableDeploy {
		return
	}
	// Tokens: component, status, timestamp
	tokens := map[string]string{
		"component": string(dt),
		"status":    "started",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"duration":  "", // not applicable yet
		"errors":    "",
	}
	msg := renderTemplate(m.NotifyMsgDeployStarted, tokens)
	if strings.TrimSpace(msg) == "" {
		msg = fmt.Sprintf("Deployment started: %s", dt)
	}
	color := parseHexColor(m.NotifyColorDeployStarted, 0x2563EB)
	title := fmt.Sprintf("Deployment: %s", dt)
	embed := discord.NewEmbed(title, msg, color, "SDSM")
	m.DiscordNotify("", embed)
}

func (m *Manager) notifyDeployComplete(dt DeployType, duration time.Duration, errs []string) {
	if m == nil || !m.NotifyEnableDeploy {
		return
	}
	durStr := duration.Truncate(time.Millisecond).String()
	tokens := map[string]string{
		"component": string(dt),
		"duration":  durStr,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	var msg string
	var colorHex string
	if len(errs) > 0 {
		tokens["status"] = "completed-error"
		tokens["errors"] = strings.Join(errs, "; ")
		msg = renderTemplate(m.NotifyMsgDeployCompletedError, tokens)
		colorHex = m.NotifyColorDeployCompletedError
		if strings.TrimSpace(msg) == "" {
			msg = fmt.Sprintf("Deployment completed with errors: %s in %s (%s)", dt, durStr, tokens["errors"])
		}
	} else {
		tokens["status"] = "completed"
		tokens["errors"] = ""
		msg = renderTemplate(m.NotifyMsgDeployCompleted, tokens)
		colorHex = m.NotifyColorDeployCompleted
		if strings.TrimSpace(msg) == "" {
			msg = fmt.Sprintf("Deployment completed: %s in %s", dt, durStr)
		}
	}
	color := parseHexColor(colorHex, defaultColorForEvent("update-completed"))
	title := fmt.Sprintf("Deployment: %s", dt)
	embed := discord.NewEmbed(title, msg, color, "SDSM")
	m.DiscordNotify("", embed)
}

// Server lifecycle notifications
func (m *Manager) NotifyServerEvent(s *models.Server, event, detail string) {
	if m == nil || s == nil {
		return
	}
	// Determine whether this event should be notified based on prefs
	if !m.shouldNotifyServerEvent(s, event) {
		return
	}
	// Effective template & color
	msgTemplate, colorHex := m.effectiveTemplateAndColor(s, event)
	// Token substitution
	ts := map[string]string{
		"server_name": s.Name,
		"event":       event,
		"detail":      detail,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	rendered := renderTemplate(msgTemplate, ts)
	if strings.TrimSpace(rendered) == "" {
		// Fallback if empty after substitution
		if detail == "" {
			rendered = fmt.Sprintf("Server %s event: %s", s.Name, event)
		} else {
			rendered = detail
		}
	}
	// Determine embed title (consistent prefix)
	title := fmt.Sprintf("Server %s: %s", s.Name, event)
	color := parseHexColor(colorHex, defaultColorForEvent(event))
	embed := discord.NewEmbed(title, rendered, color, "SDSM")
	// Route to server webhook when present; otherwise fall back to manager default
	target := strings.TrimSpace(s.DiscordWebhook)
	if target == "" {
		target = strings.TrimSpace(m.DiscordDefaultWebhook)
	}
	m.DiscordNotifyTo(target, "", embed)
}

// shouldNotifyServerEvent returns true if the provided event should be sent based on effective prefs.
func (m *Manager) shouldNotifyServerEvent(s *models.Server, event string) bool {
	if m == nil || s == nil {
		return false
	}
	// Default to using manager defaults when server has not explicitly opted out
	useDefaults := s.NotifyUseManagerDefaults
	// If legacy config where all fields are zero-values, prefer defaults
	if !useDefaults && strings.TrimSpace(s.DiscordWebhook) == "" && !s.NotifyEnable && !s.NotifyOnStart && !s.NotifyOnStopping && !s.NotifyOnStopped && !s.NotifyOnRestart && !s.NotifyOnUpdateStarted && !s.NotifyOnUpdateCompleted && !s.NotifyOnUpdateFailed {
		useDefaults = true
	}
	if useDefaults {
		if !m.NotifyEnableServer {
			return false
		}
		switch event {
		case "started":
			return m.NotifyOnStart
		case "stopping":
			return m.NotifyOnStopping
		case "stopped":
			return m.NotifyOnStopped
		case "restart-scheduled", "restart-pending", "restarting":
			return m.NotifyOnRestart
		case "update-started":
			return m.NotifyOnUpdateStarted
		case "update-completed":
			return m.NotifyOnUpdateCompleted
		case "update-failed":
			return m.NotifyOnUpdateFailed
		default:
			return m.NotifyEnableServer
		}
	}
	// Server-specific overrides
	if !s.NotifyEnable {
		return false
	}
	switch event {
	case "started":
		return s.NotifyOnStart
	case "stopping":
		return s.NotifyOnStopping
	case "stopped":
		return s.NotifyOnStopped
	case "restart-scheduled", "restart-pending", "restarting":
		return s.NotifyOnRestart
	case "update-started":
		return s.NotifyOnUpdateStarted
	case "update-completed":
		return s.NotifyOnUpdateCompleted
	case "update-failed":
		return s.NotifyOnUpdateFailed
	default:
		return s.NotifyEnable
	}
}

// effectiveTemplateAndColor returns the message template and color hex for the event using
// server overrides when not using manager defaults; otherwise manager defaults.
func (m *Manager) effectiveTemplateAndColor(s *models.Server, event string) (string, string) {
	if m == nil || s == nil {
		return "", ""
	}
	useDefaults := s.NotifyUseManagerDefaults
	if useDefaults {
		switch event {
		case "started":
			return m.NotifyMsgStart, m.NotifyColorStart
		case "stopping":
			return m.NotifyMsgStopping, m.NotifyColorStopping
		case "stopped":
			return m.NotifyMsgStopped, m.NotifyColorStopped
		case "restart-scheduled", "restart-pending", "restarting":
			return m.NotifyMsgRestart, m.NotifyColorRestart
		case "update-started":
			return m.NotifyMsgUpdateStarted, m.NotifyColorUpdateStarted
		case "update-completed":
			return m.NotifyMsgUpdateCompleted, m.NotifyColorUpdateCompleted
		case "update-failed":
			return m.NotifyMsgUpdateFailed, m.NotifyColorUpdateFailed
		default:
			return m.NotifyMsgStart, m.NotifyColorStart // generic fallback
		}
	}
	// Server overrides
	switch event {
	case "started":
		return firstNonEmpty(s.NotifyMsgStart, m.NotifyMsgStart), firstNonEmpty(s.NotifyColorStart, m.NotifyColorStart)
	case "stopping":
		return firstNonEmpty(s.NotifyMsgStopping, m.NotifyMsgStopping), firstNonEmpty(s.NotifyColorStopping, m.NotifyColorStopping)
	case "stopped":
		return firstNonEmpty(s.NotifyMsgStopped, m.NotifyMsgStopped), firstNonEmpty(s.NotifyColorStopped, m.NotifyColorStopped)
	case "restart-scheduled", "restart-pending", "restarting":
		return firstNonEmpty(s.NotifyMsgRestart, m.NotifyMsgRestart), firstNonEmpty(s.NotifyColorRestart, m.NotifyColorRestart)
	case "update-started":
		return firstNonEmpty(s.NotifyMsgUpdateStarted, m.NotifyMsgUpdateStarted), firstNonEmpty(s.NotifyColorUpdateStarted, m.NotifyColorUpdateStarted)
	case "update-completed":
		return firstNonEmpty(s.NotifyMsgUpdateCompleted, m.NotifyMsgUpdateCompleted), firstNonEmpty(s.NotifyColorUpdateCompleted, m.NotifyColorUpdateCompleted)
	case "update-failed":
		return firstNonEmpty(s.NotifyMsgUpdateFailed, m.NotifyMsgUpdateFailed), firstNonEmpty(s.NotifyColorUpdateFailed, m.NotifyColorUpdateFailed)
	default:
		return firstNonEmpty(s.NotifyMsgStart, m.NotifyMsgStart), firstNonEmpty(s.NotifyColorStart, m.NotifyColorStart)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// renderTemplate replaces {{token}} occurrences using provided map (case-sensitive) best-effort.
func renderTemplate(tmpl string, tokens map[string]string) string {
	out := tmpl
	for k, v := range tokens {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

// parseHexColor converts a #RRGGBB string to an int. Fallback provided on failure.
func parseHexColor(hex string, fallback int) int {
	h := strings.TrimSpace(hex)
	if len(h) == 7 && strings.HasPrefix(h, "#") {
		if n, err := strconv.ParseInt(h[1:], 16, 32); err == nil {
			return int(n)
		}
	}
	return fallback
}

// defaultColorForEvent returns hard-coded color defaults for events (legacy mapping).
func defaultColorForEvent(event string) int {
	switch event {
	case "started":
		return 0x16A34A
	case "stopping":
		return 0xF59E0B
	case "stopped":
		return 0xDC2626
	case "restart-scheduled", "restart-pending", "restarting":
		return 0xF59E0B
	case "update-started":
		return 0x2563EB
	case "update-completed":
		return 0x16A34A
	case "update-failed":
		return 0xDC2626
	default:
		return 0x2563EB
	}
}
