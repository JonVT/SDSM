package handlers

import (
	"testing"

	"sdsm/internal/manager"
	"sdsm/internal/models"
)

func TestValidateServerNameAvailable(t *testing.T) {
	mgr := &manager.Manager{Servers: []*models.Server{
		{ID: 1, Name: "Alpha"},
		{ID: 2, Name: "Bravo"},
	}}

	if err := ValidateServerNameAvailable(mgr, "Charlie", -1); err != nil {
		t.Fatalf("expected 'Charlie' to be available, got error: %v", err)
	}
	if err := ValidateServerNameAvailable(mgr, "Alpha", -1); err == nil {
		t.Fatalf("expected 'Alpha' to be unavailable (duplicate), got nil error")
	}
	// Excluding ID=1 should allow renaming Alpha to Alpha (self-update)
	if err := ValidateServerNameAvailable(mgr, "Alpha", 1); err != nil {
		t.Fatalf("expected 'Alpha' to be allowed when excluding same server ID, got: %v", err)
	}
}

func TestValidatePortAvailable(t *testing.T) {
	mgr := &manager.Manager{Servers: []*models.Server{
		{ID: 1, Port: 27015},
		{ID: 2, Port: 27021},
	}}

	// Valid and available: manager has no ports in use near 27030
	if port, suggested, err := ValidatePortAvailable(mgr, "27030", -1); err != nil || port != 27030 || suggested != 0 {
		t.Fatalf("expected port 27030 to be valid and available, got port=%d suggested=%d err=%v", port, suggested, err)
	}

	// Conflict: exact collision with existing 27021 should yield error and suggested port
	if port, suggested, err := ValidatePortAvailable(mgr, "27021", -1); err == nil {
		t.Fatalf("expected conflict for 27021 due to existing server using that port, got nil error")
	} else if port != 27021 || suggested == 0 {
		t.Fatalf("expected original port and non-zero suggested port on conflict, got port=%d suggested=%d", port, suggested)
	}

	// Non-numeric
	if _, _, err := ValidatePortAvailable(mgr, "abc", -1); err == nil {
		t.Fatalf("expected error for non-numeric port input")
	}
}

func TestSanitizeWelcome(t *testing.T) {
	raw := "  Hello\nWorld\r\n  "
	got := SanitizeWelcome(raw, 0)
	if got != "Hello World" {
		t.Fatalf("expected 'Hello World', got '%s'", got)
	}
	got2 := SanitizeWelcome("abcdef", 3)
	if got2 != "abc" {
		t.Fatalf("expected truncation to 'abc', got '%s'", got2)
	}
}

func TestValidateNewServerConfig(t *testing.T) {
	mgr := &manager.Manager{Servers: []*models.Server{
		{ID: 1, Name: "Alpha", Port: 27015},
	}}
	// Valid new server
	in := NewServerInput{
		Name:             " Charlie ",
		World:            "mars",
		StartLocation:    "base",
		StartCondition:   "normal",
		Difficulty:       "Easy",
		PortRaw:          "27030",
		MaxClientsRaw:    "16",
		SaveIntervalRaw:  "300",
		RestartDelayRaw:  "10",
		ShutdownDelayRaw: "2",
		BetaRaw:          "true",
	}
	cfg, err := ValidateNewServerConfig(mgr, in)
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
	if cfg.Name != "Charlie" || cfg.Port != 27030 || cfg.MaxClients != 16 || !cfg.Beta {
		t.Fatalf("unexpected validated values: %+v", cfg)
	}

	// Duplicate name should fail
	in.Name = "Alpha"
	if _, err := ValidateNewServerConfig(mgr, in); err == nil {
		t.Fatalf("expected error due to duplicate server name")
	}

	// Port conflict should return an error when colliding with existing server
	in.Name = "Delta"
	in.PortRaw = "27015" // exact conflict
	if _, err := ValidateNewServerConfig(mgr, in); err == nil {
		t.Fatalf("expected error due to port conflict")
	}
}
