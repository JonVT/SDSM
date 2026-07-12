package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, cfg map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sdsm.config")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func validBaseConfig() map[string]any {
	return map[string]any{
		"steam_id":                       "600760",
		"saved_path":                     "",
		"port":                           5000,
		"language":                       "english",
		"servers":                        []any{},
		"server_presets":                 []any{},
		"startup_update":                 true,
		"paths":                          map[string]any{"root_path": "/tmp/sdsm"},
		"tls_enabled":                    false,
		"tls_cert":                       "",
		"tls_key":                        "",
		"cookie_samesite":                "none",
		"ws_allowed_origins":             []any{},
		"windows_discovery_wmi_enabled":  true,
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	cfg := validBaseConfig()
	cfg["port"] = 70000

	m := &Manager{ConfigFile: writeTestConfig(t, cfg)}
	_, err := m.load()
	if err == nil || !strings.Contains(err.Error(), "port must be between 1 and 65535") {
		t.Fatalf("expected invalid port error, got %v", err)
	}
}

func TestLoadRejectsInvalidCookieSameSite(t *testing.T) {
	cfg := validBaseConfig()
	cfg["cookie_samesite"] = "banana"

	m := &Manager{ConfigFile: writeTestConfig(t, cfg)}
	_, err := m.load()
	if err == nil || !strings.Contains(err.Error(), "cookie_samesite must be one of") {
		t.Fatalf("expected cookie_samesite validation error, got %v", err)
	}
}

func TestLoadRejectsTLSEnabledWithoutCertAndKey(t *testing.T) {
	cfg := validBaseConfig()
	cfg["tls_enabled"] = true
	cfg["tls_cert"] = ""
	cfg["tls_key"] = ""

	m := &Manager{ConfigFile: writeTestConfig(t, cfg)}
	_, err := m.load()
	if err == nil || !strings.Contains(err.Error(), "tls_enabled=true requires both tls_cert and tls_key") {
		t.Fatalf("expected tls validation error, got %v", err)
	}
}

func TestLoadRejectsInvalidWSAllowedOriginsEntry(t *testing.T) {
	cfg := validBaseConfig()
	cfg["ws_allowed_origins"] = []any{"https://admin.example.com", "ftp://bad.example"}

	m := &Manager{ConfigFile: writeTestConfig(t, cfg)}
	_, err := m.load()
	if err == nil || !strings.Contains(err.Error(), "ws_allowed_origins contains invalid value") {
		t.Fatalf("expected ws_allowed_origins validation error, got %v", err)
	}
}

func TestLoadRejectsDuplicateNormalizedWSAllowedOrigins(t *testing.T) {
	cfg := validBaseConfig()
	cfg["ws_allowed_origins"] = []any{"https://admin.example.com", "HTTPS://ADMIN.EXAMPLE.COM:443"}

	m := &Manager{ConfigFile: writeTestConfig(t, cfg)}
	_, err := m.load()
	if err == nil || !strings.Contains(err.Error(), "ws_allowed_origins contains duplicate value") {
		t.Fatalf("expected duplicate ws_allowed_origins error, got %v", err)
	}
}

func TestLoadNormalizesValidWSAllowedOrigins(t *testing.T) {
	cfg := validBaseConfig()
	cfg["ws_allowed_origins"] = []any{" https://Admin.Example.com ", "http://example.org:8080", "*"}

	m := &Manager{ConfigFile: writeTestConfig(t, cfg)}
	_, err := m.load()
	if err != nil {
		t.Fatalf("expected valid config to load, got %v", err)
	}

	want := []string{"https://admin.example.com:443", "http://example.org:8080", "*"}
	if !reflect.DeepEqual(m.WSAllowedOrigins, want) {
		t.Fatalf("normalized ws_allowed_origins mismatch: got %v want %v", m.WSAllowedOrigins, want)
	}
}
