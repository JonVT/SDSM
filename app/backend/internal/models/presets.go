package models

// ServerPreset describes a reusable configuration profile for the Create Server form.
type ServerPreset struct {
	Key                string                 `json:"key"`
	Label              string                 `json:"label,omitempty"`
	Description        string                 `json:"description,omitempty"`
	World              string                 `json:"world,omitempty"`
	StartLocation      string                 `json:"start_location,omitempty"`
	StartCondition     string                 `json:"start_condition,omitempty"`
	Difficulty         string                 `json:"difficulty,omitempty"`
	DifficultyKeywords []string               `json:"difficulty_keywords,omitempty"`
	Beta               *bool                  `json:"beta,omitempty"`
	Fields             map[string]interface{} `json:"fields,omitempty"`
	Checkboxes         map[string]bool        `json:"checkboxes,omitempty"`
	Order              int                    `json:"order,omitempty"`
}
