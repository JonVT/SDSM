package manager

import (
	"testing"

	"github.com/gin-gonic/gin"

	cards "sdsm/app/backend/internal/cards"
)

func TestManagerLogsCardFetchDataIncludesRole(t *testing.T) {
	card := managerLogsCard{}
	data, err := card.FetchData(&cards.Request{Payload: gin.H{"role": "admin"}})
	if err != nil {
		t.Fatalf("FetchData returned error: %v", err)
	}
	if got, _ := data["role"].(string); got != "admin" {
		t.Fatalf("expected role=admin, got %q", got)
	}
}
