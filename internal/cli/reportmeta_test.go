package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScenarioResult_CarriesTheScenario(t *testing.T) {
	sc := ScenarioConfig{
		Name: "timeout-on-create", Gremlins: []string{"timeout"}, TargetTools: targetTools{"create_note"},
		Meta: map[string]any{"description": "a create that never answers", "owner": "notes"},
	}
	res := newScenarioResult(sc)
	assert.Equal(t, "medium", res.Intensity, "the effective intensity, default included")
	assert.Equal(t, []string{"create_note"}, res.TargetTools)
	assert.Equal(t, "a create that never answers", res.Meta["description"])

	data, err := json.Marshal(newScenarioResult(ScenarioConfig{Name: "plain", Gremlins: []string{"latency"}, Intensity: "high"}))
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"meta"`)
	assert.NotContains(t, string(data), `"target_tools"`)
	assert.Contains(t, string(data), `"intensity":"high"`)
}

func TestCIResult_SchemaVersionComesFirst(t *testing.T) {
	data, err := json.Marshal(newCIResult(&CIConfig{Meta: map[string]any{"suite": "nightly"}}))
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(data, []byte(`{"schema_version":2,`)), string(data))
	assert.Contains(t, string(data), `"meta":{"suite":"nightly"}`)
}

func TestReadCIResult_SchemaVersions(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "r.json")
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}
	r, err := readCIResult(write(`{"scenarios":[{"name":"a"}],"passed":true}`))
	require.NoError(t, err, "a v0.3 report has no schema_version: it is version 1")
	assert.Equal(t, 1, r.version())

	r, err = readCIResult(write(`{"schema_version":2,"scenarios":[{"name":"a","meta":{"description":"d"}}],"passed":true}`))
	require.NoError(t, err)
	assert.Equal(t, "d", r.Scenarios[0].Meta["description"])

	_, err = readCIResult(write(`{"schema_version":3,"scenarios":[],"passed":true}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version 3")
	assert.Contains(t, err.Error(), "upgrade gremlyn")
}

func TestRenderSlack(t *testing.T) {
	pass := scenario("total-outage", []string{"timeout"}, 62, true, 3)
	pass.Meta = map[string]any{"description": "every tool times out"}
	fail := scenario("inject-link", []string{"injection"}, 0, true, 1, "answer check failed: links_only_to")
	fail.Answer = &AnswerResult{Checks: []chaos.AnswerCheck{{Check: "links_only_to", Evidence: "https://evil.example"}}}
	none := scenario("no-call", []string{"latency"}, 0, false, 0, "no gremlin was ever crossed")
	fresh := scenario("brand-new", []string{"loop"}, 70, true, 2)
	cur := &CIResult{Scenarios: []ScenarioResult{pass, fail, none, fresh}}
	base := &CIResult{Scenarios: []ScenarioResult{
		scenario("total-outage", nil, 58, true, 3), scenario("inject-link", nil, 80, true, 1), scenario("no-call", nil, 50, true, 1),
	}}

	var b strings.Builder
	renderSlack(&b, cur, base)
	assert.Equal(t, strings.Join([]string{
		"✅ total-outage (every tool times out) 62 ▲4 · measured 3/3",
		"❌ inject-link 0 ▼80 · measured 1/1 · 1 failure",
		"⚠️ no-call not measured · measured 0/0 · 1 failure",
		"✅ brand-new 70 new · measured 2/2",
	}, "\n")+"\n", b.String())
	assert.NotContains(t, b.String(), "evil.example", "the slack line carries no evidence")

	b.Reset()
	renderSlack(&b, &CIResult{Scenarios: []ScenarioResult{pass}}, nil)
	assert.Equal(t, "✅ total-outage (every tool times out) 62 · measured 3/3\n", b.String())
}

func TestArenaReportCmd_Formats(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "demo", "report.json")
	for _, tt := range []struct{ format, want string }{
		{"markdown", "Gremlyn: agent resilience"},
		{"slack", "measured "},
	} {
		cmd := NewArenaReportCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{path, "--format", tt.format})
		require.NoError(t, cmd.Execute())
		assert.Contains(t, out.String(), tt.want)
	}
	cmd := NewArenaReportCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{path, "--format", "html"})
	assert.Error(t, cmd.Execute(), "an unknown format is refused")
}
