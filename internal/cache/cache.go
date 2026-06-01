package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func fiberyHome(account string) (string, error) {
	var base string
	if h := os.Getenv("FIBERY_HOME"); h != "" {
		base = h
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".fibery")
	}
	if account != "" {
		return filepath.Join(base, account), nil
	}
	return base, nil
}

func schemaPath(account string) (string, error) {
	dir, err := fiberyHome(account)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "schema.json"), nil
}

func HasSchema(account string) bool {
	path, err := schemaPath(account)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// SchemaModTime returns the schema cache's last-modified time so callers can
// warn when the schema has aged out.
func SchemaModTime(account string) (time.Time, error) {
	path, err := schemaPath(account)
	if err != nil {
		return time.Time{}, fmt.Errorf("cache.SchemaModTime: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("cache.SchemaModTime: %w", err)
	}
	return info.ModTime(), nil
}

func LoadSchema(account string) (map[string]any, error) {
	path, err := schemaPath(account)
	if err != nil {
		return nil, fmt.Errorf("cache.LoadSchema: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cache.LoadSchema: %w", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("cache.LoadSchema: parse: %w", err)
	}
	return schema, nil
}

func SaveSchema(schema map[string]any, account string) error {
	dir, err := fiberyHome(account)
	if err != nil {
		return fmt.Errorf("cache.SaveSchema: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cache.SaveSchema: mkdir: %w", err)
	}
	path := filepath.Join(dir, "schema.json")
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("cache.SaveSchema: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
