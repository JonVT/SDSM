package manager

import (
	"fmt"
	"strings"
	"time"

	"sdsm/internal/integrations/discord"
	"sdsm/internal/models"
)

// DiscordNotify posts a simple embed or content message to the manager's default webhook.
// It is best-effort; errors are logged to the manager log and otherwise ignored.
func (m *Manager) DiscordNotify(content string, embeds ...discord.Embed) {
	if m == nil || m.Log == nil {
		return
	}
	wh := strings.TrimSpace(m.DiscordDefaultWebhook)
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
	title := fmt.Sprintf("Deployment Started: %s", dt)
	desc := fmt.Sprintf("Component deployment initiated for %s.", dt)
	embed := discord.NewEmbed(title, desc, 0x2563EB, "SDSM")
	m.DiscordNotify("", embed)
}

func (m *Manager) notifyDeployComplete(dt DeployType, duration time.Duration, errs []string) {
	var color int = 0x16A34A // green
	status := "Completed"
	var desc string
	if len(errs) > 0 {
		color = 0xDC2626 // red
		status = "Completed With Errors"
		desc = fmt.Sprintf("%s finished in %s with errors: %s", dt, duration.Truncate(time.Millisecond), strings.Join(errs, "; "))
	} else {
		desc = fmt.Sprintf("%s finished successfully in %s.", dt, duration.Truncate(time.Millisecond))
	}
	embed := discord.NewEmbed(fmt.Sprintf("Deployment %s: %s", status, dt), desc, color, "SDSM")
	m.DiscordNotify("", embed)
}

// Server lifecycle notifications
func (m *Manager) NotifyServerEvent(s *models.Server, event, detail string) {
	if m == nil || s == nil {
		return
	}
	color := 0x2563EB // default blue
	switch event {
	case "started":
		color = 0x16A34A
	case "stopping":
		color = 0xF59E0B
	case "stopped":
		color = 0xDC2626
	case "restart-scheduled":
		color = 0xF59E0B
	case "restarting":
		color = 0xF59E0B
	case "update-started":
		color = 0x2563EB
	case "update-completed":
		color = 0x16A34A
	case "update-failed":
		color = 0xDC2626
	}
	title := fmt.Sprintf("Server %s: %s", s.Name, event)
	if detail == "" {
		detail = fmt.Sprintf("Server %s event: %s", s.Name, event)
	}
	embed := discord.NewEmbed(title, detail, color, "SDSM")
	m.DiscordNotify("", embed)
}
