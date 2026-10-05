package steam

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetSteamAppState(t *testing.T) {
	dir := t.TempDir()
	apps := filepath.Join(dir, "steamapps")
	for _, p := range []string{"downloading/600760", "temp/600760"} {
		if err := os.MkdirAll(filepath.Join(apps, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(apps, "appmanifest_600760.acf")
	if err := os.WriteFile(manifest, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := resetSteamAppState(dir, "600760"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{manifest, filepath.Join(apps, "downloading", "600760"), filepath.Join(apps, "temp", "600760")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed", p)
		}
	}
}

func TestEnsureWritableTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "beta")
	sub := filepath.Join(dir, "steamapps")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "a.acf")
	if err := os.WriteFile(f, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := ensureWritableTree(dir); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(sub); fi.Mode().Perm()&0o700 != 0o700 {
		t.Fatalf("dir not writable: %v", fi.Mode())
	}
	if fi, _ := os.Stat(f); fi.Mode().Perm()&0o600 != 0o600 {
		t.Fatalf("file not writable: %v", fi.Mode())
	}
}
