package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The version must land on stdout: `v=$(fibery version)` has to work, and a
// formula/CI check that captures stdout must see it.
func TestVersionGoesToStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	versionCmd.SetOut(&out)
	versionCmd.SetErr(&errOut)
	t.Cleanup(func() { versionCmd.SetOut(nil); versionCmd.SetErr(nil) })

	assert.NoError(t, versionCmd.RunE(versionCmd, nil))
	assert.Contains(t, out.String(), "fibery ")
	assert.Empty(t, errOut.String())
}
