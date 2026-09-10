package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_FromEnv(t *testing.T) {
	t.Setenv("FIBERY_API_TOKEN", "tok123")
	t.Setenv("FIBERY_WORKSPACE", "myws")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "tok123", cfg.APIToken)
	assert.Equal(t, "myws", cfg.Workspace)
}

func TestLoad_FromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FIBERY_HOME", dir)
	t.Setenv("FIBERY_API_TOKEN", "")
	t.Setenv("FIBERY_WORKSPACE", "")

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.yaml"),
		[]byte("api_token: filetoken\nworkspace: filews\n"),
		0600,
	))

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "filetoken", cfg.APIToken)
	assert.Equal(t, "filews", cfg.Workspace)
}

func TestConfig_BaseURL(t *testing.T) {
	cfg := &config.Config{Workspace: "acme"}
	assert.Equal(t, "https://acme.fibery.io", cfg.BaseURL())
}

func TestConfig_Validate(t *testing.T) {
	assert.Error(t, (&config.Config{}).Validate())
	assert.Error(t, (&config.Config{APIToken: "x"}).Validate())
	assert.NoError(t, (&config.Config{APIToken: "x", Workspace: "ws"}).Validate())
}

func TestSave_CreatesConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FIBERY_HOME", dir)

	require.NoError(t, config.Save("mytoken", "myws", ""))

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "mytoken")
	assert.Contains(t, string(data), "myws")

	info, err := os.Stat(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSave_AccountSubdir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FIBERY_HOME", dir)
	t.Setenv("FIBERY_API_TOKEN", "")
	t.Setenv("FIBERY_WORKSPACE", "")

	require.NoError(t, config.Save("defaulttok", "defaultws", ""))
	require.NoError(t, config.Save("personaltok", "personalws", "personal"))

	// Default lives at root
	cfgDefault, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "defaulttok", cfgDefault.APIToken)
	assert.Equal(t, "defaultws", cfgDefault.Workspace)

	// Personal lives in subdir — isolated from default
	cfgPersonal, err := config.Load("personal")
	require.NoError(t, err)
	assert.Equal(t, "personaltok", cfgPersonal.APIToken)
	assert.Equal(t, "personalws", cfgPersonal.Workspace)

	// Subdir file exists at the right path
	_, err = os.Stat(filepath.Join(dir, "personal", "config.yaml"))
	require.NoError(t, err)
}

func TestLoad_EnvOverFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FIBERY_HOME", dir)
	t.Setenv("FIBERY_API_TOKEN", "envtoken")
	t.Setenv("FIBERY_WORKSPACE", "")

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.yaml"),
		[]byte("api_token: filetoken\nworkspace: filews\n"),
		0600,
	))

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "envtoken", cfg.APIToken) // env wins
	assert.Equal(t, "filews", cfg.Workspace)  // file fills in
}
