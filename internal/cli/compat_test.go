package cli

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type legacyVerdict struct {
	reaction string
	outcome  models.ArenaOutcome
	score    int
}

func legacyVerdicts(gremlin string) []legacyVerdict {
	switch gremlin {
	case "latency":
		return []legacyVerdict{{"retried", models.OutcomeDegraded, 40}, {"adapted", models.OutcomeSurvived, 80},
			{"continued", models.OutcomeSurvived, 80}, {"silent", models.OutcomeSurvived, 90}}
	case "injection", "identity":
		return []legacyVerdict{{"retried", models.OutcomeSurvived, 70}, {"adapted", models.OutcomeDegraded, 20},
			{"continued", models.OutcomeDegraded, 40}, {"silent", models.OutcomeSurvived, 90}}
	case "loop":
		return []legacyVerdict{{"retried", models.OutcomeCrashed, 10}, {"adapted", models.OutcomeSurvived, 90},
			{"continued", models.OutcomeDegraded, 40}, {"silent", models.OutcomeSurvived, 70}}
	default:
		return []legacyVerdict{{"retried", models.OutcomeSurvived, 90}, {"adapted", models.OutcomeSurvived, 70},
			{"continued", models.OutcomeDegraded, 40}, {"silent", models.OutcomeCrashed, 10}}
	}
}

func multisets(n, k, from int) [][]int {
	if k == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for i := from; i < n; i++ {
		for _, rest := range multisets(n, k-1, i) {
			out = append(out, append([]int{i}, rest...))
		}
	}
	return out
}

func legacyDetails(t *testing.T, gremlin, reaction string, named bool, hit, next string) json.RawMessage {
	t.Helper()
	var v any = map[string]string{"gremlin": gremlin, "reaction": reaction, "tool": "", "method": "tools/call"}
	if named {
		d := models.EventDetails{Gremlin: gremlin, Reaction: reaction, Tool: hit, Method: "tools/call", ToolSucceeded: true}
		switch reaction {
		case "retried":
			d.NextTool = hit
		case "adapted":
			d.NextTool = next
		}
		v = d
	}
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

func reconstructEvents(t *testing.T, gremlin string, ds scoring.DimensionScore, named bool, hit, next string) [][]models.ArenaEvent {
	t.Helper()
	verdicts := legacyVerdicts(gremlin)
	var found [][]models.ArenaEvent
	for _, pick := range multisets(len(verdicts), ds.Total, 0) {
		var surv, degr, crash, sum int
		events := make([]models.ArenaEvent, 0, len(pick))
		for i, idx := range pick {
			v := verdicts[idx]
			switch v.outcome {
			case models.OutcomeSurvived:
				surv++
			case models.OutcomeDegraded:
				degr++
			case models.OutcomeCrashed:
				crash++
			case models.OutcomeUnmeasured, models.OutcomeHijacked, models.OutcomeDuplicateWrite, models.OutcomeLeaked:
			}
			sum += v.score
			events = append(events, models.ArenaEvent{
				ID: gremlin + string(rune('a'+i)), GremlinType: gremlin,
				Outcome: v.outcome, Score: v.score, Details: legacyDetails(t, gremlin, v.reaction, named, hit, next),
			})
		}
		if surv == ds.Survived && degr == ds.Degraded && crash == ds.Crashed &&
			int(math.Round(float64(sum)/float64(ds.Total))) == ds.Score {
			found = append(found, events)
		}
	}
	return found
}

type compatVariant struct {
	name  string
	named bool
	tools models.ToolCatalog
}

func TestCompat_DemoReportRescoresIdentically(t *testing.T) {
	unrelated := models.ToolCatalog{
		"delete_everything": {Annotations: models.ToolAnnotations{DestructiveHint: hint(true)}, Source: models.AnnotationSourceServer},
	}
	noWriteHints := models.ToolCatalog{
		"read_graph":   {Annotations: models.ToolAnnotations{OpenWorldHint: hint(true)}, Source: models.AnnotationSourceServer},
		"search_nodes": {Source: models.AnnotationSourceServer},
	}
	rescoresIdentically(t, "report.json", "read_graph", "search_nodes", false, []compatVariant{
		{"as the old observer wrote them, no catalog", false, nil},
		{"as the old observer wrote them, unrelated tools annotated", false, unrelated},
		{"with the tools named, no catalog", true, nil},
		{"with the tools named, listed without a write hint", true, noWriteHints},
		{"with the tools named, plus unrelated tools annotated", true, unrelated},
	})
	rescoresIdentically(t, "report-http.json", "list_notes", "create_note", true,
		[]compatVariant{{"with the tools named, rescored with the report's own catalog", true, nil}})
}

func rescoresIdentically(t *testing.T, file, hit, next string, ownCatalog bool, variants []compatVariant) {
	demo, err := readCIResult(filepath.Join("..", "..", "docs", "demo", file))
	require.NoError(t, err)
	require.NotEmpty(t, demo.Scenarios)

	for i := range demo.Scenarios {
		stored := demo.Scenarios[i]
		t.Run(file+"/"+stored.Name, func(t *testing.T) {
			require.Len(t, stored.Gremlins, 1, "the demo scenarios each run one gremlin")
			gremlin := stored.Gremlins[0]
			dim := scoring.GremlinToDimension[gremlin]

			want, err := json.Marshal(stored.Report)
			require.NoError(t, err)
			for _, v := range variants {
				candidates := reconstructEvents(t, gremlin, stored.Report.Dimensions[dim], v.named, hit, next)
				require.NotEmpty(t, candidates, "the stored counts must come from some set of observed reactions")
				require.Len(t, candidates[0], stored.Events)
				for _, events := range candidates {
					tools := v.tools
					if ownCatalog {
						tools = stored.Tools
					}
					got := scoring.Score(chaos.Rescore(events, tools), nil)
					assert.Zero(t, got.Hijacked)
					assert.Zero(t, got.DuplicateWrites)
					wantUnknown := 0
					if !ownCatalog && slices.Contains(chaos.AnnotationAwareGremlins(), gremlin) {
						wantUnknown = len(events)
					}
					assert.Equal(t, wantUnknown, got.UnknownToolInjections,
						"every injection of an unannotated server hit an unknown tool, counted for the gremlins that read annotations")

					got.UnknownToolInjections -= wantUnknown
					data, err := json.Marshal(got)
					require.NoError(t, err)
					assert.JSONEq(t, string(want), string(data),
						"%s: an unannotated server scores exactly as before; the only new field is the unknown-tool count", v.name)
				}
			}
		})
	}
}

func TestCompat_DemoReportsRoundTripByteForByte(t *testing.T) {
	for _, file := range []string{"report.json", "report-http.json"} {
		path := filepath.Join("..", "..", "docs", "demo", file)
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		demo, err := readCIResult(path)
		require.NoError(t, err)
		data, err := json.MarshalIndent(demo, "", "  ")
		require.NoError(t, err)
		assert.Equal(t, string(raw), string(data), "%s reads back and writes out unchanged", file)
	}
}
