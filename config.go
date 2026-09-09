package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Settings live outside any walkthrough: they are about this machine, not this
// pull request. Which editor to jump into, which theme to open in, whether the
// server may reach the network for the vendored assets.
type Settings struct {
	Version     int    `json:"version"`
	IDE         string `json:"ide"`
	IDEPath     string `json:"idePath,omitempty"`
	IDECommand  string `json:"ideCommand,omitempty"`
	Theme       string `json:"theme"`
	Accent      string `json:"accent"`
	Port        int    `json:"port"`
	OpenBrowser bool   `json:"openBrowser"`
	Offline     bool   `json:"offline"`
}

const settingsVersion = 1

// The accents the design ships with. Anything else typed in by hand is left
// alone: it is the reader's page.
var accents = []string{
	"oklch(0.52 0.14 255)",
	"oklch(0.58 0.14 25)",
	"oklch(0.5 0.11 190)",
	"oklch(0.48 0.12 300)",
}

func defaultSettings() Settings {
	return Settings{
		Version:     settingsVersion,
		IDE:         "auto",
		Theme:       "auto",
		Accent:      accents[0],
		Port:        0,
		OpenBrowser: true,
		Offline:     false,
	}
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "code-walkthrough", "settings.json"), nil
}

var settingsMu sync.Mutex

// LoadSettings reads the settings file, filling in anything missing and writing
// the file back on first run so there is something to edit by hand.
func LoadSettings() (Settings, string, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	p, err := settingsPath()
	if err != nil {
		return defaultSettings(), "", err
	}
	s := defaultSettings()
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		s.IDE = firstAvailableIDE()
		_ = writeSettings(p, s)
		return s, p, nil
	}
	if err != nil {
		return s, p, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return defaultSettings(), p, fmt.Errorf("%s is not valid JSON, falling back to the defaults: %w", p, err)
	}
	normalize(&s)
	return s, p, nil
}

func SaveSettings(s Settings) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	p, err := settingsPath()
	if err != nil {
		return s, err
	}
	normalize(&s)
	return s, writeSettings(p, s)
}

func writeSettings(p string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(body, '\n'), 0o644)
}

func normalize(s *Settings) {
	d := defaultSettings()
	s.Version = settingsVersion
	if s.IDE == "" {
		s.IDE = d.IDE
	}
	if s.Theme != "light" && s.Theme != "dark" {
		s.Theme = "auto"
	}
	if s.Accent == "" {
		s.Accent = d.Accent
	}
	if s.Port < 0 || s.Port > 65535 {
		s.Port = 0
	}
}
