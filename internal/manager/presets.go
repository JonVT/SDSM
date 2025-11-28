package manager

import (
	"sort"
	"strings"
	"unicode"

	"sdsm/internal/models"
)

// GetServerPresets returns a defensive copy of configured presets with defaults applied.
func (m *Manager) GetServerPresets() []models.ServerPreset {
	var presets []models.ServerPreset
	if m == nil || len(m.ServerPresets) == 0 {
		presets = normalizeServerPresetList(defaultServerPresets())
	} else {
		presets = m.ServerPresets
	}
	out := make([]models.ServerPreset, len(presets))
	copy(out, presets)
	return out
}

func normalizeServerPresetList(list []models.ServerPreset) []models.ServerPreset {
	if len(list) == 0 {
		return nil
	}
	normalized := make([]models.ServerPreset, 0, len(list))
	seen := make(map[string]struct{})
	for _, preset := range list {
		key := strings.TrimSpace(preset.Key)
		if key == "" {
			continue
		}
		lower := strings.ToLower(key)
		if _, exists := seen[lower]; exists {
			continue
		}
		clone := preset
		clone.Key = key
		clone.Label = strings.TrimSpace(clone.Label)
		if clone.Label == "" {
			clone.Label = fallbackPresetLabel(key)
		}
		if clone.Fields == nil {
			clone.Fields = map[string]interface{}{}
		}
		if clone.Checkboxes == nil {
			clone.Checkboxes = map[string]bool{}
		}
		normalized = append(normalized, clone)
		seen[lower] = struct{}{}
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].Order < normalized[j].Order
	})
	return normalized
}

func fallbackPresetLabel(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Preset"
	}
	runes := []rune(trimmed)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func ptrBool(v bool) *bool {
	b := v
	return &b
}

func defaultServerPresets() []models.ServerPreset {
	return []models.ServerPreset{
		{
			Key:                "builder",
			Label:              "Builder",
			Description:        "Creative-friendly sandbox tuned for collaborative builds.",
			World:              "Mars",
			StartCondition:     "Standard",
			Difficulty:         "Creative",
			DifficultyKeywords: []string{"creative"},
			Beta:               ptrBool(false),
			Fields: map[string]interface{}{
				"max_clients":          6,
				"save_interval":        120,
				"welcome_message":      "Creative sandbox for collaborative builds.",
				"welcome_back_message": "Welcome back, visionary builder!",
			},
			Checkboxes: map[string]bool{
				"auto_save":                true,
				"player_saves":             true,
				"auto_pause":               true,
				"auto_start":               false,
				"auto_update":              true,
				"delete_skeleton_on_decay": false,
			},
			Order: 1,
		},
		{
			Key:                "beginner",
			Label:              "Beginner",
			Description:        "Gentle start with forgiving defaults for new crews.",
			World:              "Mars",
			StartCondition:     "Standard",
			Difficulty:         "Easy",
			DifficultyKeywords: []string{"easy", "casual", "standard", "relaxed"},
			Beta:               ptrBool(false),
			Fields: map[string]interface{}{
				"max_clients":          8,
				"save_interval":        180,
				"welcome_message":      "Welcome aboard! Build at your own pace.",
				"welcome_back_message": "Welcome back, engineer!",
			},
			Checkboxes: map[string]bool{
				"auto_save":                true,
				"player_saves":             true,
				"auto_pause":               true,
				"auto_start":               false,
				"auto_update":              true,
				"delete_skeleton_on_decay": false,
			},
			Order: 2,
		},
		{
			Key:                "normal",
			Label:              "Normal",
			Description:        "Balanced progression with auto start/update enabled.",
			World:              "Moon",
			StartCondition:     "Standard",
			Difficulty:         "Normal",
			DifficultyKeywords: []string{"normal", "default", "standard"},
			Beta:               ptrBool(false),
			Fields: map[string]interface{}{
				"max_clients":           10,
				"save_interval":         300,
				"welcome_message":       "Welcome to our outpost. Have fun!",
				"welcome_back_message":  "Suit up! Time to build.",
				"welcome_delay_seconds": 1,
			},
			Checkboxes: map[string]bool{
				"auto_save":                true,
				"player_saves":             true,
				"auto_pause":               true,
				"auto_start":               true,
				"auto_update":              true,
				"delete_skeleton_on_decay": false,
			},
			Order: 3,
		},
		{
			Key:                "hardcore",
			Label:              "Hardcore",
			Description:        "High-stakes survival with stricter automation toggles.",
			World:              "Vulcan",
			StartCondition:     "Brutal",
			Difficulty:         "Stationeer",
			DifficultyKeywords: []string{"stationeer", "hard", "hardcore", "survival", "brutal"},
			Beta:               ptrBool(false),
			Fields: map[string]interface{}{
				"max_clients":           12,
				"save_interval":         420,
				"welcome_message":       "Hardcore server: no hand-holding.",
				"welcome_back_message":  "Back for more punishment? Let's go.",
				"welcome_delay_seconds": 0,
			},
			Checkboxes: map[string]bool{
				"auto_save":                true,
				"player_saves":             false,
				"auto_pause":               false,
				"auto_start":               true,
				"auto_update":              true,
				"delete_skeleton_on_decay": true,
			},
			Order: 4,
		},
	}
}
