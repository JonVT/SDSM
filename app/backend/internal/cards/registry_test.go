package cards

import (
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/gin-gonic/gin"

	"sdsm/app/backend/internal/models"
)

type stubCard struct {
	id       string
	template string
	slot     Slot
	screens  []Screen
	data     gin.H
	err      error
}

func (c stubCard) ID() string        { return c.id }
func (c stubCard) Template() string  { return c.template }
func (c stubCard) Screens() []Screen { return c.screens }
func (c stubCard) Slot() Slot        { return c.slot }
func (c stubCard) FetchData(req *Request) (gin.H, error) {
	if c.err != nil {
		return nil, c.err
	}
	out := gin.H{}
	for k, v := range c.data {
		out[k] = v
	}
	if req != nil && req.Payload != nil {
		for k, v := range req.Payload {
			out[k] = v
		}
	}
	return out, nil
}

func withIsolatedRegistry(t *testing.T, fn func()) {
	t.Helper()
	registryMu.Lock()
	original := make(map[Screen][]Card, len(registry))
	for k, v := range registry {
		original[k] = append([]Card(nil), v...)
	}
	registry = make(map[Screen][]Card)
	registryMu.Unlock()

	defer func() {
		registryMu.Lock()
		registry = original
		registryMu.Unlock()
	}()

	fn()
}

func TestBuildRenderablesFiltersByScreen(t *testing.T) {
	withIsolatedRegistry(t, func() {
		Register(stubCard{
			id:       "server-info",
			template: "cards/server_info.html",
			slot:     SlotPrimary,
			screens:  []Screen{ScreenServerStatus},
			data:     gin.H{"static": "ok"},
		})
		Register(stubCard{
			id:       "other-card",
			template: "cards/other.html",
			slot:     SlotPrimary,
			screens:  []Screen{"another"},
			data:     gin.H{"static": "nope"},
		})

		req := &Request{Payload: gin.H{"payload": "value"}}
		renderables := BuildRenderables(ScreenServerStatus, req)
		if len(renderables) != 1 {
			t.Fatalf("expected 1 renderable, got %d", len(renderables))
		}
		got := renderables[0]
		if got.ID != "server-info" {
			t.Fatalf("expected card ID server-info, got %s", got.ID)
		}
		if got.Template != "cards/server_info.html" {
			t.Fatalf("unexpected template %s", got.Template)
		}
		if got.Data["payload"] != "value" {
			t.Fatalf("expected payload data to pass through, got %v", got.Data["payload"])
		}
	})
}

func TestBuildRenderableByID(t *testing.T) {
	withIsolatedRegistry(t, func() {
		Register(stubCard{
			id:       "alpha",
			template: "cards/a.html",
			slot:     SlotPrimary,
			screens:  []Screen{ScreenServerStatus},
			data:     gin.H{"alpha": 1},
		})
		Register(stubCard{
			id:       "beta",
			template: "cards/b.html",
			slot:     SlotPrimary,
			screens:  []Screen{ScreenServerStatus},
			data:     gin.H{"beta": 2},
		})

		req := &Request{Payload: gin.H{"shared": "yes"}}
		renderable, ok := BuildRenderableByID(ScreenServerStatus, "beta", req)
		if !ok {
			t.Fatalf("expected card beta to be resolved")
		}
		if renderable.ID != "beta" {
			t.Fatalf("unexpected card %s", renderable.ID)
		}
		if renderable.Template != "cards/b.html" {
			t.Fatalf("unexpected template %s", renderable.Template)
		}
		if renderable.Data["beta"] != 2 {
			t.Fatalf("expected card data to include static field")
		}
		if renderable.Data["shared"] != "yes" {
			t.Fatalf("expected payload data to merge, got %v", renderable.Data["shared"])
		}

		if _, ok := BuildRenderableByID(ScreenServerStatus, "missing", req); ok {
			t.Fatalf("expected missing card lookup to fail")
		}
	})
}

func TestSafeFetchHandlesPanics(t *testing.T) {
	card := panicCard{id: "boom"}
	if data, err := safeFetch(card, &Request{}); err == nil {
		t.Fatalf("expected panic to propagate as error")
	} else if data != nil {
		t.Fatalf("expected nil data when panic occurs, got %#v", data)
	}
}

type panicCard struct {
	id string
}

func (p panicCard) ID() string                            { return p.id }
func (p panicCard) Template() string                      { return "cards/panic.html" }
func (p panicCard) Screens() []Screen                     { return []Screen{ScreenDashboard} }
func (p panicCard) Slot() Slot                            { return SlotPrimary }
func (p panicCard) FetchData(req *Request) (gin.H, error) { panic("boom") }

type capabilityStubCard struct {
	stubCard
	caps CardCapabilities
}

func (c capabilityStubCard) Capabilities() CardCapabilities { return c.caps }

func TestCardCapabilitiesAllowedRoles(t *testing.T) {
	withIsolatedRegistry(t, func() {
		Register(capabilityStubCard{
			stubCard: stubCard{
				id:       "restricted-card",
				template: "cards/restricted.html",
				slot:     SlotPrimary,
				screens:  []Screen{ScreenServerStatus},
				data:     gin.H{"ok": true},
			},
			caps: CardCapabilities{AllowedRoles: []string{"admin"}},
		})

		viewerCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		viewerCtx.Set("role", "viewer")
		viewerReq := &Request{Context: viewerCtx, Server: &models.Server{}}
		if renderables := BuildRenderables(ScreenServerStatus, viewerReq); len(renderables) != 0 {
			t.Fatalf("expected viewer to be blocked, got %d renderables", len(renderables))
		}

		adminCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		adminCtx.Set("role", "admin")
		adminReq := &Request{Context: adminCtx, Server: &models.Server{}}
		if renderables := BuildRenderables(ScreenServerStatus, adminReq); len(renderables) != 1 {
			t.Fatalf("expected admin to see card, got %d renderables", len(renderables))
		}
	})
}

func TestCardCapabilitiesRequirePlayerSaves(t *testing.T) {
	withIsolatedRegistry(t, func() {
		Register(capabilityStubCard{
			stubCard: stubCard{
				id:       "player-save-card",
				template: "cards/player_save.html",
				slot:     SlotPrimary,
				screens:  []Screen{ScreenServerStatus},
			},
			caps: CardCapabilities{RequirePlayerSaves: true},
		})

		req := &Request{Server: &models.Server{PlayerSaves: false}}
		if renderables := BuildRenderables(ScreenServerStatus, req); len(renderables) != 0 {
			t.Fatalf("expected card hidden when PlayerSaves disabled, got %d", len(renderables))
		}

		req.Server.PlayerSaves = true
		if renderables := BuildRenderables(ScreenServerStatus, req); len(renderables) != 1 {
			t.Fatalf("expected card visible when PlayerSaves enabled, got %d", len(renderables))
		}
	})
}

func TestCardCapabilitiesRequireServerRunning(t *testing.T) {
	withIsolatedRegistry(t, func() {
		card := capabilityStubCard{
			stubCard: stubCard{
				id:       "runtime-only-card",
				template: "cards/runtime_only.html",
				slot:     SlotPrimary,
				screens:  []Screen{ScreenServerStatus},
			},
			caps: CardCapabilities{RequireServerRunning: true},
		}

		server := &models.Server{}
		req := &Request{Server: server}
		if allowed := cardEnabledForRequest(card, req); allowed {
			t.Fatalf("expected card disabled when server stopped")
		}

		server.Proc = &exec.Cmd{}
		if allowed := cardEnabledForRequest(card, req); !allowed {
			t.Fatalf("expected card enabled when server running")
		}
	})
}
