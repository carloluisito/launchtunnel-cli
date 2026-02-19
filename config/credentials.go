package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirName         = ".launchtunnel"
	credentialsFile = "credentials.json"
)

// Credentials stores the user's authentication data.
type Credentials struct {
	APIKey string `json:"api_key"`
	APIURL string `json:"api_url,omitempty"`
	Email  string `json:"email,omitempty"`
}

// CredentialsPath returns the full path to the credentials file.
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, dirName, credentialsFile), nil
}

// LoadCredentials reads credentials from ~/.launchtunnel/credentials.json.
// Returns nil, nil if the file does not exist.
func LoadCredentials() (*Credentials, error) {
	p, err := CredentialsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parsing credentials: %w", err)
	}
	return &creds, nil
}

// SaveCredentials writes credentials to ~/.launchtunnel/credentials.json with 0600 permissions.
func SaveCredentials(creds *Credentials) error {
	p, err := CredentialsPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling credentials: %w", err)
	}

	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	return nil
}

// RemoveCredentials deletes the credentials file.
func RemoveCredentials() error {
	p, err := CredentialsPath()
	if err != nil {
		return err
	}

	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing credentials: %w", err)
	}
	return nil
}
