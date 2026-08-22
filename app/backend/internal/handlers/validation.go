package handlers

import (
	"fmt"
	"sdsm/app/backend/internal/manager"
	"sdsm/app/backend/internal/middleware"
	"sdsm/app/backend/internal/models"
	"strconv"
	"strings"
)

// ValidateServerNameAvailable ensures a name is non-empty and available (excluding the provided server ID).
func ValidateServerNameAvailable(mgr *manager.Manager, name string, excludeID int) error {
	if mgr == nil {
		return fmt.Errorf("manager unavailable")
	}
	trimmed := middleware.SanitizeString(name)
	if trimmed == "" {
		return fmt.Errorf("server name is required")
	}
	if !mgr.IsServerNameAvailable(trimmed, excludeID) {
		return fmt.Errorf("server name already exists")
	}
	return nil
}

// ValidatePortAvailable validates numeric port and checks uniqueness; returns (port, suggested, error).
// suggested will be >0 only when port conflict occurs.
func ValidatePortAvailable(mgr *manager.Manager, raw string, excludeID int) (int, int, error) {
	if mgr == nil {
		return 0, 0, fmt.Errorf("manager unavailable")
	}
	if strings.TrimSpace(raw) == "" {
		return 0, 0, fmt.Errorf("port required")
	}
	port, err := middleware.ValidatePort(raw)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port")
	}
	if !mgr.IsPortAvailable(port, excludeID) {
		suggested := mgr.GetNextAvailablePort(port)
		return port, suggested, fmt.Errorf("port not available")
	}
	return port, 0, nil
}

// SanitizeWelcome normalizes whitespace, removes newlines, trims, and clamps length.
func SanitizeWelcome(raw string, maxLen int) string {
	clean := middleware.SanitizeString(raw)
	clean = strings.ReplaceAll(strings.ReplaceAll(clean, "\r", " "), "\n", " ")
	clean = strings.TrimSpace(clean)
	if maxLen > 0 && len(clean) > maxLen {
		clean = clean[:maxLen]
	}
	return clean
}

// NewServerInput represents raw posted or JSON values before validation.
type NewServerInput struct {
	Name             string
	World            string
	StartLocation    string
	StartCondition   string
	Difficulty       string
	PortRaw          string
	MaxClientsRaw    string
	SaveIntervalRaw  string
	RestartDelayRaw  string
	ShutdownDelayRaw string
	BetaRaw          string
}

// ValidatedServerCreation encapsulates parsed values ready for config.
type ValidatedServerCreation struct {
	Name           string
	World          string
	StartLocation  string
	StartCondition string
	Difficulty     string
	Port           int
	MaxClients     int
	SaveInterval   int
	RestartDelay   int
	ShutdownDelay  int
	Beta           bool
}

// ValidateNewServerConfig validates required fields and ranges similar to existing handlers.
// Returns structured validated data or error with message suitable for user display.
func ValidateNewServerConfig(mgr *manager.Manager, in NewServerInput) (*ValidatedServerCreation, error) {
	if mgr == nil {
		return nil, fmt.Errorf("manager unavailable")
	}
	name := middleware.SanitizeString(in.Name)
	if name == "" {
		return nil, fmt.Errorf("server name is required")
	}
	if !mgr.IsServerNameAvailable(name, -1) {
		return nil, fmt.Errorf("server name %q already exists", name)
	}
	world := middleware.SanitizeString(in.World)
	if world == "" {
		return nil, fmt.Errorf("world selection is required")
	}
	startLoc := middleware.SanitizeString(in.StartLocation)
	if startLoc == "" {
		return nil, fmt.Errorf("start location is required")
	}
	startCond := middleware.SanitizeString(in.StartCondition)
	if startCond == "" {
		return nil, fmt.Errorf("start condition is required")
	}
	difficulty := middleware.SanitizeString(in.Difficulty)
	if difficulty == "" {
		return nil, fmt.Errorf("difficulty selection is required")
	}
	beta := strings.TrimSpace(in.BetaRaw) == "true"
	// Port
	port, _, perr := ValidatePortAvailable(mgr, in.PortRaw, -1)
	if perr != nil {
		// Provide suggested port if conflict
		if port > 0 && !mgr.IsPortAvailable(port, -1) {
			suggested := mgr.GetNextAvailablePort(port)
			return nil, fmt.Errorf("port %d not available; ports must be unique and at least 3 apart; try %d", port, suggested)
		}
		return nil, fmt.Errorf("invalid port number: %s", in.PortRaw)
	}
	// Max clients
	maxClients := 10
	if strings.TrimSpace(in.MaxClientsRaw) != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(in.MaxClientsRaw)); err == nil {
			maxClients = v
		}
	}
	if maxClients < 1 || maxClients > 100 {
		return nil, fmt.Errorf("invalid max players (1-100)")
	}
	// Save interval
	saveInterval := 300
	if strings.TrimSpace(in.SaveIntervalRaw) != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(in.SaveIntervalRaw)); err == nil {
			saveInterval = v
		}
	}
	if saveInterval < 60 || saveInterval > 3600 {
		return nil, fmt.Errorf("invalid save interval (60-3600)")
	}
	// Restart delay
	restartDelay := models.DefaultRestartDelaySeconds
	if strings.TrimSpace(in.RestartDelayRaw) != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(in.RestartDelayRaw)); err == nil {
			restartDelay = v
		}
	}
	if restartDelay < 0 || restartDelay > 3600 {
		return nil, fmt.Errorf("invalid restart delay (0-3600)")
	}
	// Shutdown delay
	shutdownDelay := 2
	if strings.TrimSpace(in.ShutdownDelayRaw) != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(in.ShutdownDelayRaw)); err == nil {
			shutdownDelay = v
		}
	}
	if shutdownDelay < 0 || shutdownDelay > 3600 {
		return nil, fmt.Errorf("invalid shutdown delay (0-3600)")
	}
	validated := &ValidatedServerCreation{
		Name:           name,
		World:          world,
		StartLocation:  startLoc,
		StartCondition: startCond,
		Difficulty:     difficulty,
		Port:           port,
		MaxClients:     maxClients,
		SaveInterval:   saveInterval,
		RestartDelay:   restartDelay,
		ShutdownDelay:  shutdownDelay,
		Beta:           beta,
	}
	return validated, nil
}

// DefaultDifficulty returns a sensible default difficulty label for the given channel, preferring "Normal" when present.
func DefaultDifficulty(mgr *manager.Manager, beta bool) string {
	if mgr == nil {
		return ""
	}
	diffs := mgr.GetDifficultiesForVersion(beta)
	if len(diffs) == 0 {
		return ""
	}
	pick := diffs[0]
	for _, d := range diffs {
		if strings.EqualFold(d, "Normal") {
			pick = d
			break
		}
	}
	return pick
}
