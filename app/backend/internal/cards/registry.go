package cards

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"sdsm/app/backend/internal/manager"
	"sdsm/app/backend/internal/models"
)

// Screen identifies a UI surface that hosts cards (e.g., server status, logs, etc.).
type Screen string

const (
	ScreenServerStatus Screen = "server_status"
	ScreenManager      Screen = "manager"
	ScreenDashboard    Screen = "dashboard"
	ScreenUsers        Screen = "users"
)

// Slot identifies a layout region on the page.
type Slot string

const (
	SlotPrimary Slot = "primary"
	SlotGrid    Slot = "grid"
	SlotFooter  Slot = "footer"
)

// Request provides contextual data when rendering a card.
type Request struct {
	Context  *gin.Context
	Server   *models.Server
	Payload  gin.H
	Manager  *manager.Manager
	Datasets *Datasets
}

// Card describes a renderable dashboard component.
type Card interface {
	ID() string
	Template() string
	Screens() []Screen
	Slot() Slot
	FetchData(*Request) (gin.H, error)
}

// Renderable is the hydrated card data sent to templates.
type Renderable struct {
	ID       string
	Template string
	Data     gin.H
	Slot     Slot
}

var (
	registryMu sync.RWMutex
	registry   = make(map[Screen][]Card)
)

// Register attaches a card to every screen it supports.
func Register(card Card) {
	if card == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, screen := range card.Screens() {
		if screen == "" {
			continue
		}
		registry[screen] = append(registry[screen], card)
	}
}

// BuildRenderables resolves cards for a screen and hydrates their templates with contextual data.
func BuildRenderables(screen Screen, req *Request) []Renderable {
	registryMu.RLock()
	cards := append([]Card(nil), registry[screen]...)
	registryMu.RUnlock()

	if len(cards) == 0 {
		return nil
	}

	renderables := make([]Renderable, 0, len(cards))
	for _, card := range cards {
		if !cardEnabledForRequest(card, req) {
			continue
		}
		data, err := safeFetch(card, req)
		if err != nil {
			log.Printf("cards: unable to fetch data for %s: %v", safeID(card), err)
			continue
		}
		if data == nil {
			data = gin.H{}
		}
		renderables = append(renderables, Renderable{
			ID:       card.ID(),
			Template: card.Template(),
			Data:     data,
			Slot:     card.Slot(),
		})
	}
	return renderables
}

// BuildRenderableByID resolves a single card by ID for the given screen.
// It returns the hydrated renderable and true when the card exists.
func BuildRenderableByID(screen Screen, cardID string, req *Request) (Renderable, bool) {
	if strings.TrimSpace(cardID) == "" {
		return Renderable{}, false
	}
	registryMu.RLock()
	cardsForScreen := append([]Card(nil), registry[screen]...)
	registryMu.RUnlock()
	for _, card := range cardsForScreen {
		if card == nil || card.ID() != cardID {
			continue
		}
		if !cardEnabledForRequest(card, req) {
			return Renderable{}, false
		}
		data, err := safeFetch(card, req)
		if err != nil {
			log.Printf("cards: unable to fetch data for %s: %v", safeID(card), err)
			return Renderable{}, false
		}
		if data == nil {
			data = gin.H{}
		}
		return Renderable{
			ID:       card.ID(),
			Template: card.Template(),
			Data:     data,
			Slot:     card.Slot(),
		}, true
	}
	return Renderable{}, false
}

// GroupRenderablesBySlot organizes renderables by their slot for easy lookup in templates.
func GroupRenderablesBySlot(renderables []Renderable) map[string][]Renderable {
	if len(renderables) == 0 {
		return nil
	}
	grouped := make(map[string][]Renderable)
	for _, r := range renderables {
		key := string(r.Slot)
		grouped[key] = append(grouped[key], r)
	}
	return grouped
}

// RenderableIDs returns the unique card IDs contained in the renderables slice (in order).
func RenderableIDs(renderables []Renderable) []string {
	if len(renderables) == 0 {
		return nil
	}
	ids := make([]string, 0, len(renderables))
	seen := make(map[string]struct{}, len(renderables))
	for _, r := range renderables {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func safeFetch(card Card, req *Request) (data gin.H, err error) {
	if card == nil {
		return nil, nil
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("cards: panic in %s.FetchData: %v", safeID(card), r)
			data = nil
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	data, err = card.FetchData(req)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return gin.H{}, nil
	}
	return data, nil
}

func safeID(card Card) string {
	if card == nil {
		return "<nil>"
	}
	if id := strings.TrimSpace(card.ID()); id != "" {
		return id
	}
	return "<unnamed-card>"
}
