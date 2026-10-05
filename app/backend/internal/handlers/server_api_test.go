package handlers

import (
	"archive/zip"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/models"
)

func writeTempSaveWithWorldXML(t *testing.T, worldXML string) string {
	t.Helper()

	tmpDir := t.TempDir()
	savePath := filepath.Join(tmpDir, "test.save")

	f, err := os.Create(savePath)
	if err != nil {
		t.Fatalf("create temp save: %v", err)
	}

	zw := zip.NewWriter(f)
	w, err := zw.Create("world.xml")
	if err != nil {
		_ = zw.Close()
		_ = f.Close()
		t.Fatalf("create world.xml in zip: %v", err)
	}
	if _, err := w.Write([]byte(worldXML)); err != nil {
		_ = zw.Close()
		_ = f.Close()
		t.Fatalf("write world.xml: %v", err)
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp save: %v", err)
	}

	return savePath
}

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

func TestParseWorldDataLaunchParamsFromSaveZip_UsesWorldDataStartLocationID(t *testing.T) {
	worldXML := `<?xml version="1.0" encoding="utf-8"?>
<WorldData>
  <StartLocation Id="MarsSpawnRoundRobin" />
  <StartCondition Id="BrutalCommunity" />
  <DifficultySetting Id="Stationeer" />
</WorldData>`

	savePath := writeTempSaveWithWorldXML(t, worldXML)

	params, err := parseWorldDataLaunchParamsFromSaveZip(savePath)
	if err != nil {
		t.Fatalf("parseWorldDataLaunchParamsFromSaveZip returned error: %v", err)
	}
	if params == nil {
		t.Fatalf("expected params, got nil")
	}
	if params.StartLocation != "MarsSpawnRoundRobin" {
		t.Fatalf("expected StartLocation MarsSpawnRoundRobin, got %q", params.StartLocation)
	}
	if params.StartCondition != "BrutalCommunity" {
		t.Fatalf("expected StartCondition BrutalCommunity, got %q", params.StartCondition)
	}
	if params.Difficulty != "Stationeer" {
		t.Fatalf("expected Difficulty Stationeer, got %q", params.Difficulty)
	}
}

func TestParseWorldDataLaunchParamsFromSaveZip_IgnoresThingSaveDataStartLocation(t *testing.T) {
	worldXML := `<?xml version="1.0" encoding="utf-8"?>
<WorldData>
  <StartLocation Id="MarsSpawnRoundRobin" />
  <AllThings>
    <ThingSaveData>
      <StartLocation Id="MarsSpawnFindersCanyon" />
    </ThingSaveData>
  </AllThings>
</WorldData>`

	savePath := writeTempSaveWithWorldXML(t, worldXML)

	params, err := parseWorldDataLaunchParamsFromSaveZip(savePath)
	if err != nil {
		t.Fatalf("parseWorldDataLaunchParamsFromSaveZip returned error: %v", err)
	}
	if params == nil {
		t.Fatalf("expected params, got nil")
	}
	if params.StartLocation != "MarsSpawnRoundRobin" {
		t.Fatalf("expected WorldData StartLocation MarsSpawnRoundRobin, got %q", params.StartLocation)
	}
}
