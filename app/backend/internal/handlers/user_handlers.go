package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sdsm/app/backend/internal/cards"
	"sdsm/app/backend/internal/manager"
	"sdsm/app/backend/internal/middleware"
	"sdsm/app/backend/internal/utils"

	"github.com/gin-gonic/gin"
)

type UserHandlers struct {
	users       *manager.UserStore
	authService *middleware.AuthService
	logger      *utils.Logger
	manager     *manager.Manager
}

type userRowView struct {
	manager.User
	RoleLabel     string
	AccessSummary string
	AccessDetails []string
	AccessState   string
	AssignedCSV   string
	CanEditAccess bool
}

type serverOption struct {
	ID   int
	Name string
}

func isJSONRequest(c *gin.Context) bool {
	ct := strings.ToLower(c.GetHeader("Content-Type"))
	return strings.Contains(ct, "application/json")
}

func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(c.GetHeader("HX-Request"), "true")
}

func wantsHTMLResponse(c *gin.Context) bool {
	if isHXRequest(c) {
		return true
	}
	accept := strings.ToLower(c.GetHeader("Accept"))
	return strings.Contains(accept, "text/html")
}

func (h *UserHandlers) serverLookup() map[int]string {
	lookup := make(map[int]string)
	if h == nil || h.manager == nil {
		return lookup
	}
	for _, srv := range h.manager.Servers {
		if srv == nil {
			continue
		}
		name := strings.TrimSpace(srv.Name)
		if name == "" {
			name = fmt.Sprintf("Server #%d", srv.ID)
		}
		lookup[srv.ID] = name
	}
	return lookup
}

func (h *UserHandlers) serverOptions() []serverOption {
	if h == nil || h.manager == nil {
		return nil
	}
	options := make([]serverOption, 0, len(h.manager.Servers))
	for _, srv := range h.manager.Servers {
		if srv == nil {
			continue
		}
		name := strings.TrimSpace(srv.Name)
		if name == "" {
			name = fmt.Sprintf("Server #%d", srv.ID)
		}
		options = append(options, serverOption{ID: srv.ID, Name: name})
	}
	sort.Slice(options, func(i, j int) bool {
		return strings.ToLower(options[i].Name) < strings.ToLower(options[j].Name)
	})
	return options
}

func (h *UserHandlers) buildUserRows(users []manager.User) []userRowView {
	lookup := h.serverLookup()
	rows := make([]userRowView, 0, len(users))
	for _, u := range users {
		rows = append(rows, h.buildUserRow(u, lookup))
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Username) < strings.ToLower(rows[j].Username)
	})
	return rows
}

func (h *UserHandlers) buildUserAssignments(users []manager.User) []gin.H {
	lookup := h.serverLookup()
	assignments := make([]gin.H, 0, len(users))
	for _, u := range users {
		assignments = append(assignments, gin.H{
			"username":         u.Username,
			"role":             string(u.Role),
			"role_label":       humanRole(u.Role),
			"assigned_all":     u.AssignedAllServers || u.Role == manager.RoleAdmin,
			"assigned_servers": describeAssignedServers(u, lookup),
			"assigned_csv":     joinInts(u.AssignedServers),
			"can_edit":         u.Role != manager.RoleAdmin,
		})
	}
	sort.Slice(assignments, func(i, j int) bool {
		ui := strings.ToLower(assignments[i]["username"].(string))
		uj := strings.ToLower(assignments[j]["username"].(string))
		return ui < uj
	})
	return assignments
}

func summarizeUsers(users []manager.User) gin.H {
	admins := 0
	operators := 0
	for _, u := range users {
		switch u.Role {
		case manager.RoleAdmin:
			admins++
		case manager.RoleOperator:
			operators++
		}
	}
	return gin.H{
		"total":     len(users),
		"admins":    admins,
		"operators": operators,
		"empty":     len(users) == 0,
	}
}

func (h *UserHandlers) buildUserRow(u manager.User, lookup map[int]string) userRowView {
	state := "partial"
	switch {
	case u.Role == manager.RoleAdmin:
		state = "admin"
	case u.AssignedAllServers:
		state = "all"
	case len(u.AssignedServers) == 0:
		state = "none"
	}
	summary, details := describeAccess(u, lookup)
	return userRowView{
		User:          u,
		RoleLabel:     humanRole(u.Role),
		AccessSummary: summary,
		AccessDetails: details,
		AccessState:   state,
		AssignedCSV:   joinInts(u.AssignedServers),
		CanEditAccess: u.Role != manager.RoleAdmin,
	}
}

func (h *UserHandlers) buildUsersCardRequest(c *gin.Context) (*cards.Request, gin.H, []userRowView) {
	if h == nil || c == nil {
		return nil, nil, nil
	}
	if h.users != nil {
		_ = h.users.Load()
	}
	var list []manager.User
	if h.users != nil {
		list = h.users.Users()
	}
	rows := h.buildUserRows(list)
	assignments := h.buildUserAssignments(list)
	serverOptions := h.serverOptions()
	serverOptionsJSON := "[]"
	if encoded, err := json.Marshal(serverOptions); err == nil {
		serverOptionsJSON = string(encoded)
	} else if h.logger != nil {
		h.logger.Write(fmt.Sprintf("UsersGET: failed to marshal server options to JSON: %v", err))
	}
	payload := gin.H{
		"users":             rows,
		"userAssignments":   assignments,
		"username":          c.GetString("username"),
		"role":              c.GetString("role"),
		"now":               time.Now(),
		"serverOptions":     serverOptions,
		"serverOptionsJSON": serverOptionsJSON,
		"userStats":         summarizeUsers(list),
		"cardIDs":           []string{},
		"cardSlots":         map[string][]cards.Renderable{},
	}
	req := &cards.Request{
		Context: c,
		Manager: h.manager,
		Payload: payload,
	}
	return req, payload, rows
}

func describeAccess(u manager.User, lookup map[int]string) (string, []string) {
	switch {
	case u.Role == manager.RoleAdmin:
		return "Admin (full access)", nil
	case u.AssignedAllServers:
		return "All servers", nil
	case len(u.AssignedServers) == 0:
		if u.Role == manager.RoleOperator {
			return "No servers assigned", nil
		}
		return "No servers", nil
	default:
		names := make([]string, 0, len(u.AssignedServers))
		for _, id := range u.AssignedServers {
			names = append(names, serverNameOrID(lookup, id))
		}
		summary := fmt.Sprintf("%d server%s", len(names), pluralSuffix(len(names)))
		return summary, names
	}
}

func describeAssignedServers(u manager.User, lookup map[int]string) []string {
	if u.Role == manager.RoleAdmin || u.AssignedAllServers {
		return nil
	}
	if len(u.AssignedServers) == 0 {
		return nil
	}
	names := make([]string, 0, len(u.AssignedServers))
	for _, id := range u.AssignedServers {
		names = append(names, serverNameOrID(lookup, id))
	}
	sort.Strings(names)
	return names
}

func serverNameOrID(lookup map[int]string, id int) string {
	if lookup != nil {
		if name, ok := lookup[id]; ok && strings.TrimSpace(name) != "" {
			return name
		}
	}
	return fmt.Sprintf("Server #%d", id)
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func humanRole(role manager.Role) string {
	switch role {
	case manager.RoleAdmin:
		return "Admin"
	case manager.RoleOperator:
		return "Operator"
	case manager.RoleViewer:
		return "Viewer"
	case manager.RoleUser:
		return "User"
	default:
		return string(role)
	}
}

// NewUserHandlers constructs handlers with optional logger (nil-safe).
func NewUserHandlers(store *manager.UserStore, auth *middleware.AuthService, logger *utils.Logger, manager *manager.Manager) *UserHandlers {
	return &UserHandlers{users: store, authService: auth, logger: logger, manager: manager}
}

func (h *UserHandlers) UsersGET(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		if h.logger != nil {
			uname := strings.TrimSpace(c.GetString("username"))
			h.logger.Write(fmt.Sprintf("UsersGET: forbidden for user '%s' (role=%s)", uname, c.GetString("role")))
		}
		c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "Admin privileges required."})
		return
	}
	cardReq, data, rows := h.buildUsersCardRequest(c)
	if data == nil {
		data = gin.H{}
	}
	if h.logger != nil {
		usernames := make([]string, 0, len(rows))
		for _, u := range rows {
			usernames = append(usernames, u.Username+":"+string(u.Role))
		}
		h.logger.Write(fmt.Sprintf("UsersGET: returning %d user(s): %s", len(rows), strings.Join(usernames, ",")))
	}
	if cardReq != nil {
		renderables := cards.BuildRenderables(cards.ScreenUsers, cardReq)
		if grouped := cards.GroupRenderablesBySlot(renderables); len(grouped) > 0 {
			data["cardSlots"] = grouped
		} else if _, ok := data["cardSlots"]; !ok {
			data["cardSlots"] = map[string][]cards.Renderable{}
		}
		if ids := cards.RenderableIDs(renderables); len(ids) > 0 {
			data["cardIDs"] = ids
		} else if _, ok := data["cardIDs"]; !ok {
			data["cardIDs"] = []string{}
		}
	} else {
		if _, ok := data["cardSlots"]; !ok {
			data["cardSlots"] = map[string][]cards.Renderable{}
		}
		if _, ok := data["cardIDs"]; !ok {
			data["cardIDs"] = []string{}
		}
	}

	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "users.html", data)
		return
	}

	data["servers"] = h.manager.Servers
	data["buildTime"] = h.manager.BuildTime()
	data["active"] = h.manager.IsActive()
	data["page"] = "users"
	data["title"] = "User Management"

	c.HTML(http.StatusOK, "frame.html", data)
}

// UsersCardGET renders a single users-page card for HTMX refresh requests.
func (h *UserHandlers) UsersCardGET(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.String(http.StatusForbidden, "admin required")
		return
	}
	cardID := strings.TrimSpace(c.Param("card_id"))
	if cardID == "" {
		c.String(http.StatusBadRequest, "card id required")
		return
	}
	cardReq, _, _ := h.buildUsersCardRequest(c)
	if cardReq == nil {
		c.String(http.StatusInternalServerError, "card context unavailable")
		return
	}
	renderable, ok := cards.BuildRenderableByID(cards.ScreenUsers, cardID, cardReq)
	if !ok {
		c.String(http.StatusNotFound, "card not found")
		return
	}
	c.HTML(http.StatusOK, renderable.Template, renderable.Data)
}

func (h *UserHandlers) ProfileGET(c *gin.Context) {
	// If the request is from HTMX, render the partial view
	if c.GetHeader("HX-Request") == "true" {
		c.HTML(http.StatusOK, "profile.html", gin.H{
			"username": c.GetString("username"),
			"now":      time.Now(),
		})
		return
	}

	// Otherwise, render the full frame, which will load the correct content via htmx
	c.HTML(http.StatusOK, "frame.html", gin.H{
		"username":  c.GetString("username"),
		"role":      c.GetString("role"),
		"servers":   h.manager.Servers,
		"buildTime": h.manager.BuildTime(),
		"active":    h.manager.IsActive(),
		"page":      "profile",
		"title":     "My Profile",
	})
}

// UsersPOST removed: legacy HTML form user management replaced by /api/users endpoints.

// --- JSON API for user management (admin only) ---

// APIUsersList returns users, optionally filtered by ?q=
func (h *UserHandlers) APIUsersList(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		if h.logger != nil {
			uname := strings.TrimSpace(c.GetString("username"))
			h.logger.Write(fmt.Sprintf("APIUsersList: forbidden for user '%s' (role=%s)", uname, c.GetString("role")))
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	// Best-effort refresh from disk to reflect any external changes
	_ = h.users.Load()
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	users := h.users.Users()
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		if q != "" {
			if !strings.Contains(strings.ToLower(u.Username), q) && !strings.Contains(strings.ToLower(string(u.Role)), q) {
				continue
			}
		}
		out = append(out, gin.H{
			"username":   u.Username,
			"role":       u.Role,
			"created_at": u.CreatedAt,
		})
	}
	if h.logger != nil {
		usernames := make([]string, 0, len(out))
		for _, obj := range out {
			if name, ok := obj["username"].(string); ok {
				if role, ok2 := obj["role"].(manager.Role); ok2 {
					usernames = append(usernames, name+":"+string(role))
				} else if roleStr, ok3 := obj["role"].(string); ok3 {
					usernames = append(usernames, name+":"+roleStr)
				}
			}
		}
		if q != "" {
			h.logger.Write(fmt.Sprintf("APIUsersList: query='%s' matched %d user(s): %s", q, len(out), strings.Join(usernames, ",")))
		} else {
			h.logger.Write(fmt.Sprintf("APIUsersList: returning %d user(s): %s", len(out), strings.Join(usernames, ",")))
		}
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type apiCreateUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (h *UserHandlers) APIUsersCreate(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	req, err := h.bindCreateUser(c)
	if err != nil {
		ToastError(c, "Add User Failed", "Invalid request payload.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	username := middleware.SanitizeString(strings.TrimSpace(req.Username))
	password := req.Password
	if len(username) < 3 || len(password) < 8 {
		ToastError(c, "Add User Failed", "Username must be at least 3 characters and password at least 8 characters.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "username >=3 and password >=8 required"})
		return
	}
	roleStr := strings.ToLower(strings.TrimSpace(req.Role))
	role := manager.RoleOperator
	switch roleStr {
	case string(manager.RoleAdmin):
		role = manager.RoleAdmin
	case string(manager.RoleOperator), "":
		role = manager.RoleOperator
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	hash, err := h.authService.HashPassword(password)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "hash failure"})
		return
	}
	if _, err := h.users.CreateUser(username, hash, role); err != nil {
		ToastError(c, "Add User Failed", err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ToastSuccess(c, "User Created", fmt.Sprintf("User %s created.", username))
	if wantsHTMLResponse(c) {
		if u, ok := h.users.Get(username); ok {
			row := h.buildUserRow(*u, h.serverLookup())
			c.HTML(http.StatusCreated, "user_row", row)
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"status": "ok"})
}

type apiSetRoleReq struct {
	Role string `json:"role"`
}

func (h *UserHandlers) APIUsersSetRole(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	req, err := h.bindRoleRequest(c)
	if err != nil {
		ToastError(c, "Update Role Failed", "Invalid request payload.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	roleStr := strings.ToLower(strings.TrimSpace(req.Role))
	if roleStr != string(manager.RoleAdmin) && roleStr != string(manager.RoleOperator) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	// Prevent demoting the last admin
	if roleStr != string(manager.RoleAdmin) {
		if u, ok := h.users.Get(username); ok && u.Role == manager.RoleAdmin {
			admins := 0
			for _, usr := range h.users.Users() {
				if usr.Role == manager.RoleAdmin {
					admins++
				}
			}
			if admins <= 1 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "at least one admin required"})
				return
			}
		}
	}
	if err := h.users.SetRole(username, manager.Role(roleStr)); err != nil {
		ToastError(c, "Update Role Failed", err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ToastSuccess(c, "Role Updated", fmt.Sprintf("%s is now %s.", username, humanRole(manager.Role(roleStr))))
	if wantsHTMLResponse(c) {
		if u, ok := h.users.Get(username); ok {
			row := h.buildUserRow(*u, h.serverLookup())
			c.HTML(http.StatusOK, "user_row", row)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// API: get/set operator server assignments
func (h *UserHandlers) APIUsersGetAssignments(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	all, list, err := h.users.GetAssignments(username)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"all": all, "servers": list})
}

type apiSetAssignmentsReq struct {
	All     bool  `json:"all"`
	Servers []int `json:"servers"`
}

func (h *UserHandlers) APIUsersSetAssignments(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	req, err := h.bindAssignmentsRequest(c)
	if err != nil {
		ToastError(c, "Update Access Failed", "Invalid request payload.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if err := h.users.SetAssignments(username, req.All, req.Servers); err != nil {
		ToastError(c, "Update Access Failed", err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ToastSuccess(c, "Access Updated", fmt.Sprintf("Updated access for %s.", username))
	if wantsHTMLResponse(c) {
		if u, ok := h.users.Get(username); ok {
			row := h.buildUserRow(*u, h.serverLookup())
			c.HTML(http.StatusOK, "user_row", row)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *UserHandlers) APIUsersDelete(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	if u, ok := h.users.Get(username); ok && u.Role == manager.RoleAdmin {
		admins := 0
		for _, usr := range h.users.Users() {
			if usr.Role == manager.RoleAdmin {
				admins++
			}
		}
		if admins <= 1 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "cannot delete last admin"})
			return
		}
	}
	if err := h.users.Delete(username); err != nil {
		ToastError(c, "Delete Failed", err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ToastSuccess(c, "User Deleted", fmt.Sprintf("User %s deleted.", username))
	if wantsHTMLResponse(c) {
		c.String(http.StatusOK, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type apiResetPasswordReq struct {
	Password string `json:"password"`
}

func (h *UserHandlers) APIUsersResetPassword(c *gin.Context) {
	if c.GetString("role") != string(manager.RoleAdmin) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	username := strings.TrimSpace(c.Param("username"))
	req, err := h.bindResetPasswordRequest(c)
	if err != nil {
		ToastError(c, "Reset Failed", "Invalid request payload.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if len(req.Password) < 8 {
		ToastError(c, "Reset Failed", "Password must be at least 8 characters.")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "password must be >= 8 chars"})
		return
	}
	hash, err := h.authService.HashPassword(req.Password)
	if err != nil {
		ToastError(c, "Reset Failed", "Unable to hash password.")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "hash failure"})
		return
	}
	if err := h.users.SetPassword(username, hash); err != nil {
		ToastError(c, "Reset Failed", err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ToastSuccess(c, "Password Reset", fmt.Sprintf("Password for %s updated.", username))
	if wantsHTMLResponse(c) {
		c.String(http.StatusOK, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *UserHandlers) bindCreateUser(c *gin.Context) (apiCreateUserReq, error) {
	var req apiCreateUserReq
	if isJSONRequest(c) {
		if err := c.ShouldBindJSON(&req); err != nil {
			return req, err
		}
		return req, nil
	}
	req.Username = c.PostForm("username")
	req.Password = c.PostForm("password")
	req.Role = c.PostForm("role")
	return req, nil
}

func (h *UserHandlers) bindRoleRequest(c *gin.Context) (apiSetRoleReq, error) {
	var req apiSetRoleReq
	if isJSONRequest(c) {
		if err := c.ShouldBindJSON(&req); err != nil {
			return req, err
		}
		return req, nil
	}
	req.Role = c.PostForm("role")
	return req, nil
}

func (h *UserHandlers) bindResetPasswordRequest(c *gin.Context) (apiResetPasswordReq, error) {
	var req apiResetPasswordReq
	if isJSONRequest(c) {
		if err := c.ShouldBindJSON(&req); err != nil {
			return req, err
		}
		return req, nil
	}
	req.Password = c.PostForm("password")
	return req, nil
}

func (h *UserHandlers) bindAssignmentsRequest(c *gin.Context) (apiSetAssignmentsReq, error) {
	var req apiSetAssignmentsReq
	if isJSONRequest(c) {
		if err := c.ShouldBindJSON(&req); err != nil {
			return req, err
		}
		return req, nil
	}
	req.All = parseTruthy(c.PostForm("assign_all"))
	servers := c.PostFormArray("servers")
	if len(servers) == 0 {
		servers = c.PostFormArray("servers[]")
	}
	for _, raw := range servers {
		id, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || id <= 0 {
			continue
		}
		req.Servers = append(req.Servers, id)
	}
	return req, nil
}

func parseTruthy(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "true", "1", "on", "yes", "all":
		return true
	default:
		return false
	}
}
