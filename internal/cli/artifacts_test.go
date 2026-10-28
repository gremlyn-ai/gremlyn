package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func artifactsRun(t *testing.T, gremlynBin, agentBin string, art artifacts, n int) ScenarioResult {
	t.Helper()
	cfg := &CIConfig{
		Agent: AgentConfig{Command: []string{agentBin, "--mode", "fragile", "--tool", "read_graph",
			"--mcp-config", PlaceholderMCPConfig, "--prompt", PlaceholderPrompt, "--answer", "The graph is empty."},
			Timeout: 60 * time.Second},
		MCPServer: MCPServerConfig{Name: "memory", Command: fakeMCPServer()},
	}
	sc := ScenarioConfig{Name: "kept run", Gremlins: []string{"corruption"}, Seed: 7, Intensity: "certain", Prompt: "read"}
	return runRepeated(context.Background(), zerolog.Nop(), gremlynBin, cfg, sc, art, n, false)
}

func assertPrivateFile(t *testing.T, path, contains string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), contains)
}

func TestArenaCI_Artifacts(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes and compiles two binaries")
	}
	gremlynBin, agentBin := buildGremlyn(t), buildRefAgent(t)

	t.Run("kept under a chosen directory, created if missing", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "ci", "artifacts")
		res := artifactsRun(t, gremlynBin, agentBin, artifacts{dir: root}, 1)
		require.NotEmpty(t, res.Artifacts)
		assert.Equal(t, root, filepath.Dir(res.Artifacts))
		assert.Regexp(t, `^kept-run-`, filepath.Base(res.Artifacts))
		assertPrivateFile(t, filepath.Join(res.Artifacts, "agent.log"), "The graph is empty.")
		assertPrivateFile(t, filepath.Join(res.Artifacts, "answer.txt"), "The graph is empty.")
		assert.FileExists(t, filepath.Join(res.Artifacts, "events.jsonl"))
		info, err := os.Stat(root)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	})

	t.Run("removed without keep", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		res := artifactsRun(t, gremlynBin, agentBin, artifacts{}, 1)
		assert.Empty(t, res.Artifacts)
		left, err := os.ReadDir(tmp)
		require.NoError(t, err)
		for _, e := range left {
			assert.NotRegexp(t, `^gremlyn-ci-`, e.Name(), "the scenario directory is removed")
		}
	})

	t.Run("each repeat keeps its own directory", func(t *testing.T) {
		root := t.TempDir()
		res := artifactsRun(t, gremlynBin, agentBin, artifacts{dir: root}, 2)
		require.Len(t, res.Runs, 2)
		assert.NotEqual(t, res.Runs[0].Artifacts, res.Runs[1].Artifacts)
		for _, r := range res.Runs {
			assert.FileExists(t, filepath.Join(r.Artifacts, "agent.log"))
		}
	})
}

func TestArtifacts_Kept(t *testing.T) {
	assert.False(t, artifacts{}.kept())
	assert.True(t, artifacts{keep: true}.kept())
	assert.True(t, artifacts{dir: "out"}.kept(), "--artifacts-dir implies --keep-artifacts")
}
