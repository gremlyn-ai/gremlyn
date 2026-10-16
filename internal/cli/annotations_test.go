package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hint(v bool) *bool { return &v }

func writeArenaYAML(t *testing.T, annotations string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "arena.yaml")
	body := fmt.Sprintf(`
agent:
  command: ["agent", "--mcp-config", "%s"]
mcp_server:
  name: notes
  command: ["./notes"]
%s
scenarios:
  - name: s1
    gremlins: [injection]
`, PlaceholderMCPConfig, annotations)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadCIConfig_AnnotationOverrides(t *testing.T) {
	tests := []struct {
		name        string
		annotations string
		wantErr     string
	}{
		{"none", "", ""},
		{"declared effects", `  annotations:
    create_note: {readOnlyHint: false, idempotentHint: false}
    delete_note: {destructiveHint: true}
    list_notes: {readOnlyHint: true}`, ""},
		{"an unknown hint is a typo, not a no-op", `  annotations:
    create_note: {readonly: true}`, `unknown hint "readonly"`},
		{"openWorldHint changes no score", `  annotations:
    list_notes: {readOnlyHint: true, openWorldHint: true}`, "openWorldHint changes no score"},
		{"an empty entry", `  annotations:
    create_note: {}`, "declare at least one of"},
		{"a tool declared twice", `  annotations:
    create_note: {readOnlyHint: false}
    create_note: {idempotentHint: false}`, "declared twice"},
		{"a list instead of a mapping", `  annotations: [create_note]`, "maps each tool name to its hints"},
		{"a hint that is not a mapping", `  annotations:
    create_note: write`, "must map hints to true or false"},
		{"a hint that is not a boolean", `  annotations:
    create_note: {readOnlyHint: maybe}`, "create_note"},
		{"a hint in the wrong case", `  annotations:
    create_note: {idempotenthint: false}`, `unknown hint "idempotenthint"`},
		{"a null hint", `  annotations:
    create_note: {idempotentHint: false, readOnlyHint: null}`, "readOnlyHint must be true or false"},
		{"a tilde hint", `  annotations:
    create_note: {idempotentHint: ~}`, "idempotentHint must be true or false"},
		{"a quoted boolean", `  annotations:
    create_note: {idempotentHint: "false"}`, "idempotentHint must be true or false"},
		{"a null entry", `  annotations:
    create_note:`, "must map hints to true or false"},
		{"read-only and destructive", `  annotations:
    list_notes: {readOnlyHint: true, destructiveHint: true}`, "a read-only tool writes nothing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadCIConfig(writeArenaYAML(t, tt.annotations))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.annotations == "" {
				assert.Empty(t, cfg.MCPServer.Annotations)
				return
			}
			assert.Equal(t, models.EffectNonIdempotentWrite, cfg.MCPServer.Annotations["create_note"].Effect())
			assert.Equal(t, models.EffectDestructive, cfg.MCPServer.Annotations["delete_note"].Effect())
			assert.Equal(t, models.EffectReadOnly, cfg.MCPServer.Annotations["list_notes"].Effect())
		})
	}
}

func TestWriteMCPConfig_PassesTheAnnotationOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	srv := MCPServerConfig{Name: "notes", Command: []string{"./notes"}, Annotations: annotationOverrides{
		"create_note": {ReadOnlyHint: hint(false), IdempotentHint: hint(false)},
	}}
	require.NoError(t, writeMCPConfig(path, "/usr/bin/gremlyn", srv,
		ScenarioConfig{Gremlins: []string{"timeout"}}, 1, "/tmp/e.jsonl", "/tmp/s.json"))

	var got struct {
		MCPServers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))
	args := got.MCPServers["notes"].Args
	i := indexOf(args, "--chaos-annotations")
	require.Positive(t, i, "the override must reach the stdio proxy")
	parsed, err := chaos.ParseAnnotationOverrides([]byte(args[i+1]))
	require.NoError(t, err, "what arena ci writes, wrap must accept")
	assert.Equal(t, map[string]models.ToolAnnotations(srv.Annotations), parsed)
	assert.Less(t, i, indexOf(args, "--"), "it is a wrap flag, not a server argument")

	require.NoError(t, writeMCPConfig(path, "/usr/bin/gremlyn", MCPServerConfig{Name: "notes", Command: []string{"./notes"}},
		ScenarioConfig{Gremlins: []string{"timeout"}}, 1, "/tmp/e.jsonl", "/tmp/s.json"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "--chaos-annotations")
	assert.NotContains(t, string(data), "--chaos-target-tool")
}

func TestWrapCmd_RejectsAnnotationsItCannotUse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"without a gremlin nothing is scored", []string{"--chaos-annotations", `{"a":{"readOnlyHint":true}}`, "--", "true"}, "applies with --chaos-gremlins only"},
		{"malformed JSON", []string{"--chaos-gremlins", "timeout", "--chaos-annotations", `{"a":`, "--", "true"}, "--chaos-annotations"},
		{"an unknown hint", []string{"--chaos-gremlins", "timeout", "--chaos-annotations", `{"a":{"readOnly":true}}`, "--", "true"}, "--chaos-annotations"},
		{"an entry with no hint", []string{"--chaos-gremlins", "timeout", "--chaos-annotations", `{"a":{}}`, "--", "true"}, "declare at least one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewWrapCmd(zerolog.Nop())
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestReplay_CarriesOnlyTheConfiguredAnnotations(t *testing.T) {
	path := writeSummary(t, chaos.SessionSummary{
		Gremlins: []string{"timeout"}, Seed: 3, Intensity: "certain",
		ServerCommand: []string{"./notes"},
		Tools: models.ToolCatalog{
			"create_note": {Annotations: models.ToolAnnotations{IdempotentHint: hint(false)}, Source: models.AnnotationSourceConfig},
			"list_notes":  {Annotations: models.ToolAnnotations{ReadOnlyHint: hint(true)}, Source: models.AnnotationSourceServer},
		},
	})
	out, _, err := runReplay(t, path)
	require.NoError(t, err)

	flags := parseWrapFlags(t, out)
	parsed, err := chaos.ParseAnnotationOverrides([]byte(flags["--chaos-annotations"]))
	require.NoError(t, err)
	assert.Equal(t, map[string]models.ToolAnnotations{"create_note": {IdempotentHint: hint(false)}}, parsed,
		"the server's own annotations come back from the server; only the user's declaration is part of the recipe")

	plain := writeSummary(t, chaos.SessionSummary{Gremlins: []string{"timeout"}, Seed: 3, ServerCommand: []string{"./notes"}})
	out, _, err = runReplay(t, plain)
	require.NoError(t, err)
	assert.NotContains(t, out, "--chaos-annotations")
}

func hijackDetails(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(models.EventDetails{
		Gremlin: "injection", Reaction: "adapted", Tool: "list_notes", Method: "tools/call", NextTool: "delete_note",
	})
	require.NoError(t, err)
	return data
}

func TestChaosSessionRecorder_RescoresWithTheRecordedAnnotations(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("GREMLYN_DATA_DIR", dataDir)
	ctx := context.Background()

	rec := newChaosSessionRecorder(ctx, chaosRecipe{
		Server: []string{"./notes"}, Gremlins: []string{"injection"}, Seed: 1, Intensity: "certain",
		Annotations: map[string]models.ToolAnnotations{"delete_note": {DestructiveHint: hint(true)}},
	}, zerolog.Nop())
	require.NotNil(t, rec)

	require.NoError(t, rec.RecordEvent(ctx, models.ArenaEvent{
		ID: "e1", GremlinType: "injection", Outcome: models.OutcomeDegraded, Score: 20, Details: hijackDetails(t),
		InjectedAt: time.Unix(1700000000, 0).UTC(),
	}))
	tools := models.ToolCatalog{
		"delete_note": {Annotations: models.ToolAnnotations{DestructiveHint: hint(true)}, Source: models.AnnotationSourceConfig},
		"list_notes":  {Annotations: models.ToolAnnotations{ReadOnlyHint: hint(true)}, Source: models.AnnotationSourceServer},
	}
	rec.finish(ctx, nil, chaos.UpstreamStats{}, tools)

	store := readBackStore(t, dataDir)
	sessions, err := store.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	got := sessions[0]
	assert.Equal(t, 1, got.GremlinsHijacked)
	assert.Equal(t, tools, got.Tools, "the annotations the run saw are part of its record")
	assert.Contains(t, string(got.Config), `"annotations"`, "the user's declaration is part of the recipe")

	var stored scoring.ResilienceReport
	require.NoError(t, json.Unmarshal(got.Results, &stored))
	assert.Equal(t, 1, stored.Hijacked)
	assert.Equal(t, 0, stored.Overall)

	events, err := store.ListBySession(ctx, got.ID)
	require.NoError(t, err)
	fromFiles, err := json.Marshal(scoring.Score(chaos.Rescore(events, got.Tools), nil))
	require.NoError(t, err)
	assert.JSONEq(t, string(got.Results), string(fromFiles), "session.json and events.jsonl alone rebuild the same report")
}

func TestRunScenario_ReportCarriesTheTools(t *testing.T) {
	res := ScenarioResult{Tools: models.ToolCatalog{"list_notes": {Annotations: models.ToolAnnotations{ReadOnlyHint: hint(true)}, Source: models.AnnotationSourceServer}}}
	data, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"tools":{"list_notes":{"annotations":{"readOnlyHint":true},"source":"server"}}`)

	data, err = json.Marshal(ScenarioResult{})
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(data), `"tools"`), "an unannotated run adds nothing to the report")
}
