package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSummary(t *testing.T, s chaos.SessionSummary) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "summary.json")
	require.NoError(t, chaos.WriteSummary(path, s))
	return path
}

func runReplay(t *testing.T, path string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := NewArenaReplayCmd(zerolog.Nop())
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{path}, args...))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestReplay_ReconstructsTheRecipe(t *testing.T) {
	path := writeSummary(t, chaos.SessionSummary{
		Gremlins:      []string{"timeout", "corruption"},
		Seed:          42,
		Intensity:     "high",
		ServerCommand: []string{"npx", "-y", "@modelcontextprotocol/server-memory"},
	})

	out, _, err := runReplay(t, path)
	require.NoError(t, err)

	assert.Contains(t, out, "gremlyn wrap")
	assert.Contains(t, out, "--chaos-gremlins timeout,corruption")
	assert.Contains(t, out, "--chaos-seed 42")
	assert.Contains(t, out, "--chaos-intensity high")
	assert.Contains(t, out, "-- npx -y @modelcontextprotocol/server-memory")
}

func TestReplay_IncludesParamsOnlyWhenOverridden(t *testing.T) {
	withParams := writeSummary(t, chaos.SessionSummary{
		Gremlins:      []string{"latency"},
		Seed:          1,
		Intensity:     "certain",
		Params:        chaos.GremlinParams{MinDelayMs: 4000, MaxDelayMs: 6000},
		ServerCommand: []string{"./srv"},
	})
	out, _, err := runReplay(t, withParams)
	require.NoError(t, err)
	assert.Contains(t, out, "--chaos-params")
	assert.Contains(t, out, "4000")

	noParams := writeSummary(t, chaos.SessionSummary{
		Gremlins: []string{"latency"}, Seed: 1, Intensity: "low", ServerCommand: []string{"./srv"},
	})
	out, _, err = runReplay(t, noParams)
	require.NoError(t, err)
	assert.NotContains(t, out, "--chaos-params",
		"an unmodified recipe must not carry a wall of zero-valued params")
}

func TestReplay_RoundTripsThroughWrapFlags(t *testing.T) {
	original := chaos.SessionSummary{
		Gremlins:      []string{"injection", "identity"},
		Seed:          7,
		Intensity:     "medium",
		Params:        chaos.GremlinParams{InjectionText: "custom"},
		ServerCommand: []string{"node", "server.js"},
	}
	out, _, err := runReplay(t, writeSummary(t, original))
	require.NoError(t, err)

	flags := parseWrapFlags(t, out)
	assert.Equal(t, "injection,identity", flags["--chaos-gremlins"])
	assert.Equal(t, "7", flags["--chaos-seed"])
	assert.Equal(t, "medium", flags["--chaos-intensity"])

	var params chaos.GremlinParams
	require.NoError(t, json.Unmarshal([]byte(flags["--chaos-params"]), &params))
	assert.Equal(t, "custom", params.InjectionText)
}

func TestReplay_RefusesAnIncompleteSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"coverage":{"injected":3}}`), 0o600))

	_, _, err := runReplay(t, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing the replay recipe")
}

func TestReplay_MarksAMissingServerCommand(t *testing.T) {
	path := writeSummary(t, chaos.SessionSummary{
		Gremlins: []string{"timeout"}, Seed: 1, Intensity: "high",
	})
	out, _, err := runReplay(t, path)
	require.NoError(t, err)
	assert.Contains(t, out, "not recorded")
}

func TestReplay_NoteIsOnStderrAndSuppressible(t *testing.T) {
	path := writeSummary(t, chaos.SessionSummary{
		Gremlins: []string{"timeout"}, Seed: 1, ServerCommand: []string{"./s"},
	})

	_, errOut, err := runReplay(t, path)
	require.NoError(t, err)
	assert.Contains(t, errOut, "score can differ", "the honesty note must be present by default")

	_, errOut, err = runReplay(t, path, "--print")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(errOut), "--print must emit only the command")
}

func TestReplay_MissingFileIsAnError(t *testing.T) {
	_, _, err := runReplay(t, filepath.Join(t.TempDir(), "nope.json"))
	require.Error(t, err)
}

func parseWrapFlags(t *testing.T, command string) map[string]string {
	t.Helper()
	flat := strings.ReplaceAll(command, "\\\n", " ")
	fields := splitRespectingQuotes(flat)
	out := map[string]string{}
	for i := 0; i < len(fields); i++ {
		if strings.HasPrefix(fields[i], "--") && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "--") && fields[i+1] != "--" {
			out[fields[i]] = strings.Trim(fields[i+1], "'")
			i++
		}
	}
	return out
}

func splitRespectingQuotes(s string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '\'':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}
