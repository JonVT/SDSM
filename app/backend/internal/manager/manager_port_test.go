package manager

import (
	"testing"

	"sdsm/app/backend/internal/models"
)

func TestGetNextAvailablePort_DefaultSpacing(t *testing.T) {
	mgr := &Manager{
		Servers: []*models.Server{
			{Port: 27016},
			{Port: 27019},
		},
	}

	port := mgr.GetNextAvailablePort(27016)
	if port != 27022 {
		t.Fatalf("expected default spacing to yield 27022, got %d", port)
	}
}

func TestGetNextAvailablePort_AlignedStartUsesSpacing(t *testing.T) {
	mgr := &Manager{
		Servers: []*models.Server{
			{Port: 27019},
		},
	}

	port := mgr.GetNextAvailablePort(27019)
	if port != 27022 {
		t.Fatalf("expected aligned start to step by 3 and return 27022, got %d", port)
	}
}

func TestGetNextAvailablePort_CustomStartUsesSequential(t *testing.T) {
	mgr := &Manager{
		Servers: []*models.Server{
			{Port: 30000},
		},
	}

	port := mgr.GetNextAvailablePort(30000)
	if port != 30001 {
		t.Fatalf("expected sequential fallback for custom start, got %d", port)
	}
}
