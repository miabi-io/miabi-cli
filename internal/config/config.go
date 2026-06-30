// Package config resolves the CLI's connection context — the panel URL, API
// token, and active workspace — from flags, environment, and ~/.miabi/config.yaml,
// in that precedence. The token is never logged.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WorkspaceRef is the active workspace persisted by `miabi workspace switch`.
// ID is the durable key (paths address by it); Name is the unique handle and
// DisplayName the free-text label, kept for display.
type WorkspaceRef struct {
	ID          uint   `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	DisplayName string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
}

// Identity is the authenticated user, captured at `miabi login` for offline
// display (`whoami`/`workspace show`). The token remains the source of truth.
type Identity struct {
	Name     string `yaml:"name,omitempty" json:"name,omitempty"` // display name
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Email    string `yaml:"email,omitempty" json:"email,omitempty"`
}

// File mirrors ~/.miabi/config.yaml.
type File struct {
	URL       string        `yaml:"url,omitempty"`
	Token     string        `yaml:"token,omitempty"`
	User      *Identity     `yaml:"user,omitempty"`
	Workspace *WorkspaceRef `yaml:"workspace,omitempty"`
}

// Path returns the config file location (honoring MIABI_CONFIG, else
// ~/.miabi/config.yaml).
func Path() string {
	if p := os.Getenv("MIABI_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".miabi-config.yaml"
	}
	return filepath.Join(home, ".miabi", "config.yaml")
}

// Load reads the config file. A missing file is not an error (returns an empty
// File), so CI that uses only env vars never needs one.
func Load() (*File, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return &File{}, nil
		}
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(), err)
	}
	return &f, nil
}

// Save writes the config file, creating ~/.miabi with 0700 and the file with
// 0600 (it holds a token).
func Save(f *File) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Effective is the resolved context a command runs against.
type Effective struct {
	URL       string
	Token     string
	Workspace *WorkspaceRef // from the file; may be nil
}

// Resolve applies precedence flags → env (MIABI_URL/MIABI_TOKEN) → file.
func Resolve(flagURL, flagToken string) (*Effective, error) {
	f, err := Load()
	if err != nil {
		return nil, err
	}
	url := firstNonEmpty(flagURL, os.Getenv("MIABI_URL"), f.URL)
	token := firstNonEmpty(flagToken, os.Getenv("MIABI_TOKEN"), f.Token)
	if url == "" {
		return nil, fmt.Errorf("no panel URL configured — pass --url, set MIABI_URL, or run `miabi login`")
	}
	if token == "" {
		return nil, fmt.Errorf("no API token configured — pass --token, set MIABI_TOKEN, or run `miabi login`")
	}
	return &Effective{URL: url, Token: token, Workspace: f.Workspace}, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
