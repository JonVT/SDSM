package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/manager"
	"sdsm/app/backend/internal/models"
)

type testCard struct {
	id       string
	template string
	screens  []cards.Screen
}

func (c testCard) ID() string              { return c.id }
func (c testCard) Template() string        { return c.template }
func (c testCard) Screens() []cards.Screen { return c.screens }
func (c testCard) Slot() cards.Slot        { return cards.SlotPrimary }
func (c testCard) FetchData(req *cards.Request) (gin.H, error) {
	return gin.H{"value": "fresh"}, nil
}

func setupTestRouter(t *testing.T, handler *ManagerHandlers, tpl string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tmpl := template.Must(template.New("test").Parse(tpl))
	r.SetHTMLTemplate(tmpl)
	r.Use(func(c *gin.Context) {
		c.Set("username", "tester")
		c.Set("role", "admin")
	})
	r.GET("/server/:server_id/cards/:card_id", handler.ServerCardGET)
	return r
}

func TestServerCardGETSuccess(t *testing.T) {
	cardID := "test-card-success"
	templateName := "cards/" + cardID + ".html"
	cards.Register(testCard{id: cardID, template: templateName, screens: []cards.Screen{cards.ScreenServerStatus}})

	server := &models.Server{ID: 42, Name: "Alpha"}
	mgr := &manager.Manager{Servers: []*models.Server{server}}

	handler := &ManagerHandlers{manager: mgr}
	handler.cardRequestBuilder = func(c *gin.Context, s *models.Server, username interface{}, errMsg string) (*cards.Request, gin.H) {
		payload := gin.H{"server": s}
		return &cards.Request{Context: c, Server: s, Payload: payload}, payload
	}

	tpl := `{{define "` + templateName + `"}}stub {{.value}}{{end}}`
	router := setupTestRouter(t, handler, tpl)

	req := httptest.NewRequest(http.MethodGet, "/server/42/cards/"+cardID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "stub fresh" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestServerCardGETMissingCard(t *testing.T) {
	server := &models.Server{ID: 7, Name: "Bravo"}
	mgr := &manager.Manager{Servers: []*models.Server{server}}

	handler := &ManagerHandlers{manager: mgr}
	handler.cardRequestBuilder = func(c *gin.Context, s *models.Server, username interface{}, errMsg string) (*cards.Request, gin.H) {
		payload := gin.H{"server": s}
		return &cards.Request{Context: c, Server: s, Payload: payload}, payload
	}

	tpl := `{{define "cards/any.html"}}noop{{end}}`
	router := setupTestRouter(t, handler, tpl)

	req := httptest.NewRequest(http.MethodGet, "/server/7/cards/missing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
