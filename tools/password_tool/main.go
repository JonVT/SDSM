package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sdsm/internal/manager"
	"sdsm/internal/middleware"
	"sdsm/internal/utils"

	"golang.org/x/term"
)

// minimalConfig models just the portion of sdsm.config we need (root_path)
type minimalConfig struct {
	Paths struct {
		RootPath string `json:"root_path"`
	} `json:"paths"`
}

func main() {
	configPath := flag.String("config", "sdsm.config", "Path to sdsm.config")
	username := flag.String("username", "admin", "Username to update or create")
	password := flag.String("password", "", "New password (leave blank to type securely)")
	flag.Parse()

	if strings.TrimSpace(*username) == "" {
		fmt.Fprintln(os.Stderr, "username cannot be empty")
		os.Exit(1)
	}

	cfgPath, err := filepath.Abs(strings.TrimSpace(*configPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to resolve config path: %v\n", err)
		os.Exit(1)
	}

	rootPath, err := loadRootPath(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	if rootPath == "" {
		fmt.Fprintln(os.Stderr, "root_path is not set in config; set it or pass --config pointing to the correct file")
		os.Exit(1)
	}

	paths := utils.NewPaths(rootPath)
	store := manager.NewUserStore(paths)
	if err := store.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load users.json: %v\n", err)
		os.Exit(1)
	}

	pwd, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "password error: %v\n", err)
		os.Exit(1)
	}

	auth := middleware.NewAuthService()
	hash, err := auth.HashPassword(pwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to hash password: %v\n", err)
		os.Exit(1)
	}

	if err := store.SetPassword(*username, hash); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "user not found") {
			if _, createErr := store.CreateUser(*username, hash, manager.RoleAdmin); createErr != nil {
				fmt.Fprintf(os.Stderr, "failed to create user: %v\n", createErr)
				os.Exit(1)
			}
			fmt.Printf("Created user %s with admin role.\n", *username)
		} else {
			fmt.Fprintf(os.Stderr, "failed to update password: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("Updated password for %s.\n", *username)
	}

	fmt.Printf("users.json: %s\n", paths.UsersFile())
}

func loadRootPath(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	var cfg minimalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	root := strings.TrimSpace(cfg.Paths.RootPath)
	return root, nil
}

func resolvePassword(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "" {
		if len(trimmed) < 8 {
			return "", fmt.Errorf("password must be at least 8 characters")
		}
		return trimmed, nil
	}

	first, err := promptPassword("Enter new password: ")
	if err != nil {
		return "", err
	}
	second, err := promptPassword("Confirm password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", fmt.Errorf("passwords do not match")
	}
	if len(first) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	return first, nil
}

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		bytes, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(bytes)), nil
	}

	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}
