package dashboard

import (
	"testing"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

func TestDashboardServerDeckCardFetchDataIncludesRole(t *testing.T) {
	card := dashboardServerDeckCard{}
	data, err := card.FetchData(&cards.Request{Payload: gin.H{"role": "admin"}})
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if got, _ := data["role"].(string); got != "admin" {
		t.Fatalf("expected role=admin, got %q", got)
	}
}

func TestDashboardServerTilesCardFetchDataIncludesRole(t *testing.T) {
	card := dashboardServerTilesCard{}
	data, err := card.FetchData(&cards.Request{Payload: gin.H{"role": "admin"}})
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if got, _ := data["role"].(string); got != "admin" {
		t.Fatalf("expected role=admin, got %q", got)
	}
}
