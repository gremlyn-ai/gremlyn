package cli

import (
	"bytes"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCmd(t *testing.T) {
	cmd := NewVersionCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "gremlyn version")
	assert.Contains(t, output, "dev")
}

func TestVersionCmd_CustomValues(t *testing.T) {
	origVersion, origCommit, origDate := Version, Commit, BuildDate
	defer func() {
		Version, Commit, BuildDate = origVersion, origCommit, origDate
	}()

	Version = "1.0.0"
	Commit = "abc123"
	BuildDate = "2025-01-01"

	cmd := NewVersionCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "abc123")
	assert.Contains(t, output, "2025-01-01")
}

func TestResolveVersion(t *testing.T) {
	v, c, d := resolveVersion("0.2.0", "abc1234", "2026-01-01", &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, true)
	assert.Equal(t, []string{"0.2.0", "abc1234", "2026-01-01"}, []string{v, c, d}, "release ldflags win")

	v, c, d = resolveVersion("dev", "unknown", "unknown", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, true)
	assert.Equal(t, []string{"0.1.0", "unknown", "unknown"}, []string{v, c, d}, "go install reports the module version")

	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "e8b0c8369fa9ee51"}, {Key: "vcs.time", Value: "2026-10-07T19:00:00Z"},
	}}
	v, c, d = resolveVersion("dev", "unknown", "unknown", info, true)
	assert.Equal(t, []string{"dev", "e8b0c83", "2026-10-07T19:00:00Z"}, []string{v, c, d}, "a source build reports its commit")

	v, _, _ = resolveVersion("dev", "unknown", "unknown", nil, false)
	assert.Equal(t, "dev", v)
}
