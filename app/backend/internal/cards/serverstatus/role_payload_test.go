package serverstatus

import (
	"testing"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/models"
)

func TestServerStatusInfoCardFetchDataIncludesRole(t *testing.T) {
	card := serverStatusInfoCard{}
	data, err := card.FetchData(&cards.Request{
		Server:  &models.Server{},
		Payload: gin.H{"role": "admin"},
	})
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if got, _ := data["role"].(string); got != "admin" {
		t.Fatalf("expected role=admin, got %q", got)
	}
}

func TestServerStatusLogsCardFetchDataIncludesRole(t *testing.T) {
	card := serverStatusLogsCard{}
	data, err := card.FetchData(&cards.Request{
		Server:  &models.Server{},
		Payload: gin.H{"role": "admin"},
	})
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if got, _ := data["role"].(string); got != "admin" {
		t.Fatalf("expected role=admin, got %q", got)
	}
}

func TestServerStatusPlayersCardFetchDataIncludesRole(t *testing.T) {
	card := serverStatusPlayersCard{}
	data, err := card.FetchData(&cards.Request{
		Server:  &models.Server{},
		Payload: gin.H{"role": "admin"},
	})
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if got, _ := data["role"].(string); got != "admin" {
		t.Fatalf("expected role=admin, got %q", got)
	}
}
