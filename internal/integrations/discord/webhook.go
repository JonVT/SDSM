package discord

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

// Embed represents a minimal Discord embed payload.
// See: https://discord.com/developers/docs/resources/channel#embed-object-embed-structure
// We intentionally keep this minimal for MVP.
type Embed struct {
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Color       int         `json:"color,omitempty"`
	Timestamp   string      `json:"timestamp,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}

type EmbedFooter struct {
	Text string `json:"text,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// WebhookPayload is the JSON body for Discord webhooks.
type WebhookPayload struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// Post sends a JSON webhook to the provided URL. Returns the HTTP status code and any error.
func Post(webhookURL string, payload WebhookPayload) (int, error) {
	if webhookURL == "" {
		return 0, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// NewEmbed creates an embed with timestamp set to now in RFC3339 format.
func NewEmbed(title, description string, color int, footer string) Embed {
	return Embed{
		Title:       title,
		Description: description,
		Color:       color,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Footer:      &EmbedFooter{Text: footer},
	}
}
