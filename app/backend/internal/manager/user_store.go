package manager

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sdsm/app/backend/internal/utils"
)

// Role represents a user's role in the system.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
	RoleUser     Role = "user"
)

// User holds authentication data and role for an account.
type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	// Operator access control
	AssignedAllServers bool  `json:"assigned_all_servers,omitempty"`
	AssignedServers    []int `json:"assigned_servers,omitempty"`
}

// UserStore manages persistent users with a JSON file backend.
type UserStore struct {
	path  string
	mu    sync.RWMutex
	users map[string]*User
}

// NewUserStore initializes a user store at the configured path.
func NewUserStore(paths *utils.Paths) *UserStore {
	p := paths.UsersFile()
	return &UserStore{path: p, users: make(map[string]*User)}
}

// Path returns the absolute path to the users.json backing file used by this store.
func (s *UserStore) Path() string {
	return s.path
}

// Load reads users from disk; missing file is treated as empty store.
func (s *UserStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users = make(map[string]*User)

	if s.path == "" {
		return errors.New("user store path not set")
	}
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		// Ensure parent directory exists
		_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var list []*User
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	for _, u := range list {
		if u != nil && u.Username != "" {
			s.users[u.Username] = u
		}
	}
	return nil
}

// saveLocked writes users to disk atomically with 0600 permissions.
// Caller MUST hold s.mu (write lock) before calling.
func (s *UserStore) saveLocked() error {
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Save acquires a write lock and persists users to disk.
func (s *UserStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// IsEmpty reports whether no users exist.
func (s *UserStore) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) == 0
}

// Get returns a copy of the user by username.
func (s *UserStore) Get(username string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	if !ok {
		return nil, false
	}
	// Return a shallow copy to avoid external mutation
	copy := *u
	return &copy, true
}

// CreateUser creates a new user with a pre-hashed password.
func (s *UserStore) CreateUser(username, passwordHash string, role Role) (*User, error) {
	if username == "" || passwordHash == "" {
		return nil, errors.New("username and password hash required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[username]; exists {
		return nil, errors.New("user already exists")
	}
	u := &User{Username: username, PasswordHash: passwordHash, Role: role, CreatedAt: time.Now()}
	s.users[username] = u
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return u, nil
}

// SetPassword updates the password hash for a user.
func (s *UserStore) SetPassword(username, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return errors.New("user not found")
	}
	u.PasswordHash = passwordHash
	return s.saveLocked()
}

// SetRole updates a user's role.
func (s *UserStore) SetRole(username string, role Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return errors.New("user not found")
	}
	u.Role = role
	return s.saveLocked()
}

// GetAssignments returns whether the user is assigned to all servers and the explicit list.
func (s *UserStore) GetAssignments(username string) (bool, []int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	if !ok {
		return false, nil, errors.New("user not found")
	}
	// Copy slice to avoid external mutation
	var list []int
	if len(u.AssignedServers) > 0 {
		list = make([]int, len(u.AssignedServers))
		copy(list, u.AssignedServers)
	}
	return u.AssignedAllServers, list, nil
}

// SetAssignments updates operator server assignments.
func (s *UserStore) SetAssignments(username string, all bool, servers []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return errors.New("user not found")
	}
	u.AssignedAllServers = all
	if all {
		u.AssignedServers = nil
	} else {
		// de-dup and sanitize
		uniq := make(map[int]struct{}, len(servers))
		out := make([]int, 0, len(servers))
		for _, id := range servers {
			if id <= 0 {
				continue
			}
			if _, seen := uniq[id]; !seen {
				uniq[id] = struct{}{}
				out = append(out, id)
			}
		}
		u.AssignedServers = out
	}
	return s.saveLocked()
}

// CanAccess reports whether an operator has access to the given server.
// Admin checks should be handled by callers.
func (s *UserStore) CanAccess(username string, serverID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	if !ok {
		return false
	}
	if u.AssignedAllServers {
		return true
	}
	for _, id := range u.AssignedServers {
		if id == serverID {
			return true
		}
	}
	return false
}

// Users returns a snapshot list of users.
func (s *UserStore) Users() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, *u)
	}
	return out
}

// Delete removes a user by username.
func (s *UserStore) Delete(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[username]; !ok {
		return errors.New("user not found")
	}
	delete(s.users, username)
	return s.saveLocked()
}

// AdminCount returns the number of users with admin role.
func (s *UserStore) AdminCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, u := range s.users {
		if u.Role == RoleAdmin {
			count++
		}
	}
	return count
}
