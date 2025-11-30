package handlers

import (
	"net/url"
	"testing"

	"sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/models"
)

func TestApplyServerCardToggleSnapshot(t *testing.T) {
	options := cards.ToggleableCardsForScreen(cards.ScreenServerStatus)
	if len(options) < 2 {
		t.Skip("not enough toggleable cards registered")
	}
	first := options[0].ID
	second := options[1].ID

	server := &models.Server{CardToggles: map[string]bool{"custom-card": true}}
	selections := map[string]bool{
		first:  true,
		second: false,
	}

	applyServerCardToggleSnapshot(server, selections)

	if !server.CardToggles[first] {
		t.Fatalf("expected %s to be true after snapshot", first)
	}
	if server.CardToggles[second] {
		t.Fatalf("expected %s to be false after snapshot", second)
	}
	if !server.CardToggles["custom-card"] {
		t.Fatalf("existing non-screen toggle should be preserved")
	}
	if len(options) > 2 {
		third := options[2].ID
		if server.CardToggles[third] {
			t.Fatalf("expected unmentioned card %s to default to false", third)
		}
	}
}

func TestApplyServerCardTogglePartial(t *testing.T) {
	options := cards.ToggleableCardsForScreen(cards.ScreenServerStatus)
	if len(options) < 2 {
		t.Skip("not enough toggleable cards registered")
	}
	first := options[0].ID
	second := options[1].ID

	server := &models.Server{CardToggles: map[string]bool{first: false}}
	updates := map[string]bool{
		first:          true,
		second:         false,
		"unknown-card": true,
	}

	applyServerCardTogglePartial(server, updates)

	if !server.CardToggles[first] {
		t.Fatalf("expected %s to be updated to true", first)
	}
	if val, ok := server.CardToggles[second]; !ok || val {
		t.Fatalf("expected %s to be set to false", second)
	}
	if _, ok := server.CardToggles["unknown-card"]; ok {
		t.Fatalf("unknown card IDs should be ignored")
	}
}

func TestParseCardToggleSnapshotInputSources(t *testing.T) {
	form := url.Values{}
	form.Set("card_toggle_present", "1")
	form.Add("card_toggle", " server-status-info ")

	selections, provided, usedJSONMap := parseCardToggleSnapshotInput(nil, form)
	if !provided {
		t.Fatalf("expected form submission to be treated as snapshot")
	}
	if usedJSONMap {
		t.Fatalf("form-only snapshot should not consume JSON map")
	}
	if len(selections) != 1 || !selections["server-status-info"] {
		t.Fatalf("expected selections to capture trimmed card IDs")
	}

	jsonBody := map[string]any{
		"card_toggle_present": true,
		"card_toggles": map[string]any{
			"server-status-info": "true",
			"server-status-chat": "0",
		},
	}
	selections, provided, usedJSONMap = parseCardToggleSnapshotInput(jsonBody, nil)
	if !provided || !usedJSONMap {
		t.Fatalf("expected JSON map snapshot to be consumed")
	}
	if !selections["server-status-info"] || selections["server-status-chat"] {
		t.Fatalf("expected JSON map values to be parsed into booleans")
	}
	if partial := parseCardTogglePartialInput(jsonBody, usedJSONMap); partial != nil {
		t.Fatalf("consumed JSON map should not be re-applied as partial")
	}
}

func TestParseCardTogglePartialInput(t *testing.T) {
	jsonBody := map[string]any{
		"card_toggles": map[string]any{
			"server-status-info": true,
			"server-status-chat": false,
		},
	}
	partial := parseCardTogglePartialInput(jsonBody, false)
	if len(partial) != 2 {
		t.Fatalf("expected both entries to be parsed")
	}
	if !partial["server-status-info"] || partial["server-status-chat"] {
		t.Fatalf("expected parsed booleans to reflect source values")
	}
}
