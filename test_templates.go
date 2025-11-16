package main

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"path/filepath"
	"sdsm/ui"
	"strings"
)

func main() {
	fmt.Println("Running template parsing test...")

	funcMap := template.FuncMap{
		"add":       func(a, b int) int { return a + b },
		"has":       func(slice []string, item string) bool { return false },
		"dict":      func(values ...interface{}) map[string]interface{} { return nil },
		"initials":  func(s string) string { return "T" },
		"buildTime": func() string { return "test" },
	}

	t := template.New("").Funcs(funcMap)

	err := fs.WalkDir(ui.Assets, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".html") {
			log.Printf("Parsing: %s", path)
			content, readErr := fs.ReadFile(ui.Assets, path)
			if readErr != nil {
				log.Fatalf("FATAL: failed to read template %s: %v", path, readErr)
			}
			// Use the base name of the file as the template name
			_, parseErr := t.New(filepath.Base(path)).Parse(string(content))
			if parseErr != nil {
				// This is the critical part - it will print the exact error
				log.Fatalf("FATAL: failed to parse template %s: %v", path, parseErr)
			}
		}
		return nil
	})

	if err != nil {
		log.Fatalf("FATAL: Error walking templates directory: %v", err)
	}

	// Check for specific templates that should be in the set
	requiredTemplates := []string{"login.html", "manager.html", "frame.html", "dashboard.html", "setup.html", "error.html", "toast.html", "icon_user.html"}
	for _, name := range requiredTemplates {
		if t.Lookup(name) == nil {
			log.Fatalf("FATAL: embedded template missing from set: %s", name)
		} else {
			log.Printf("OK: Found template %s", name)
		}
	}

	log.Println("SUCCESS: All templates parsed and checked successfully.")
}
