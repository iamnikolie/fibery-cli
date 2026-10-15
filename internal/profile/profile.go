package profile

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Edges lists, per role, the relation field names to walk from an entity.
type Edges struct {
	Parent   []string `yaml:"parent,omitempty"`
	Children []string `yaml:"children,omitempty"`
	Blockers []string `yaml:"blockers,omitempty"`
	Code     []string `yaml:"code,omitempty"`
	Quality  []string `yaml:"quality,omitempty"`
	Service  []string `yaml:"service,omitempty"`
}

// Profile is the curation rule set for one database.
type Profile struct {
	Keep     []string `yaml:"keep"`     // scalar/enum/user field names to surface
	Docs     []string `yaml:"docs"`     // document field names whose bodies to inline
	Comments string   `yaml:"comments"` // "full" | "none"
	Edges    Edges    `yaml:"edges"`
}

// Profiles is the on-disk context.yaml: a map keyed by "Space/Database".
type Profiles struct {
	Profiles map[string]Profile `yaml:"profiles"`
}

func (p *Profiles) Get(db string) (Profile, bool) {
	prof, ok := p.Profiles[db]
	return prof, ok
}

func (p *Profiles) Set(db string, prof Profile) {
	if p.Profiles == nil {
		p.Profiles = map[string]Profile{}
	}
	p.Profiles[db] = prof
}

// fiberyHome mirrors internal/config and internal/cache so profiles are isolated
// per account exactly like config.yaml and schema.json.
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

func contextPath(account string) (string, error) {
	dir, err := fiberyHome(account)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "context.yaml"), nil
}

// Load reads the account's context.yaml. A missing file is not an error — it
// returns an empty, ready-to-populate Profiles.
func Load(account string) (*Profiles, error) {
	path, err := contextPath(account)
	if err != nil {
		return nil, fmt.Errorf("profile.Load: %w", err)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Profiles{Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("profile.Load: %w", err)
	}
	var p Profiles
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("profile.Load: parse: %w", err)
	}
	if p.Profiles == nil {
		p.Profiles = map[string]Profile{}
	}
	return &p, nil
}

// Save writes profiles to the account's context.yaml (0600, dir 0700).
func Save(p *Profiles, account string) error {
	dir, err := fiberyHome(account)
	if err != nil {
		return fmt.Errorf("profile.Save: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("profile.Save: mkdir: %w", err)
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("profile.Save: marshal: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "context.yaml"), data, 0600)
}
