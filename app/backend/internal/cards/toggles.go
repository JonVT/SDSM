package cards

// ToggleOption describes a card that can be enabled/disabled per screen.
type ToggleOption struct {
	Screen      Screen
	ID          string
	Label       string
	Description string
}

var toggleableCards = []ToggleOption{
	{Screen: ScreenServerStatus, ID: "server-status-info", Label: "Status Overview", Description: "Server identity, world, and port metadata."},
	{Screen: ScreenServerStatus, ID: "server-status-control", Label: "Power Controls", Description: "Start, stop, pause, and update actions."},
	{Screen: ScreenServerStatus, ID: "server-status-players", Label: "Players", Description: "Connected players and recent history."},
	{Screen: ScreenServerStatus, ID: "server-status-chat", Label: "Chat", Description: "Live chat stream and command input."},
	{Screen: ScreenServerStatus, ID: "server-status-saves", Label: "Saves Browser", Description: "Manual, auto, quick, and player saves."},
	{Screen: ScreenServerStatus, ID: "server-status-config", Label: "Configuration", Description: "World, automation, and networking settings."},
	{Screen: ScreenServerStatus, ID: "server-status-discord", Label: "Discord Overrides", Description: "Per-server notification preferences."},
	{Screen: ScreenServerStatus, ID: "server-status-logs", Label: "Logs", Description: "Console output, log downloads, and diagnostics."},
}

// ToggleableCardsForScreen returns the list of toggleable cards for a given screen.
func ToggleableCardsForScreen(screen Screen) []ToggleOption {
	result := make([]ToggleOption, 0, len(toggleableCards))
	for _, opt := range toggleableCards {
		if opt.Screen == screen {
			result = append(result, opt)
		}
	}
	return result
}
