package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerMode_Valid(t *testing.T) {
	assert.True(t, ServerModeWrap.Valid())
	assert.True(t, ServerModeProxy.Valid())
	assert.True(t, ServerModeCloud.Valid())
	assert.False(t, ServerMode("unknown").Valid())
	assert.False(t, ServerMode("").Valid())
}

func TestActionTaken_Valid(t *testing.T) {
	assert.True(t, ActionAllowed.Valid())
	assert.True(t, ActionBlocked.Valid())
	assert.True(t, ActionRedacted.Valid())
	assert.True(t, ActionAlerted.Valid())
	assert.False(t, ActionTaken("nope").Valid())
}

func TestRuleAction_Valid(t *testing.T) {
	validActions := []RuleAction{
		RuleActionAllow, RuleActionBlock, RuleActionBlockAndAlert,
		RuleActionRedact, RuleActionRedactAndAlert, RuleActionThrottle,
		RuleActionPauseAndRequestApproval, RuleActionLogOnly,
	}
	for _, a := range validActions {
		assert.True(t, a.Valid(), "expected %q to be valid", a)
	}
	assert.False(t, RuleAction("invalid").Valid())
}

func TestAlertSeverity_Valid(t *testing.T) {
	assert.True(t, AlertSeverityCritical.Valid())
	assert.True(t, AlertSeverityHigh.Valid())
	assert.True(t, AlertSeverityMedium.Valid())
	assert.True(t, AlertSeverityLow.Valid())
	assert.False(t, AlertSeverity("extreme").Valid())
}

func TestDirection_Valid(t *testing.T) {
	assert.True(t, DirectionOutgoing.Valid())
	assert.True(t, DirectionIncoming.Valid())
	assert.True(t, DirectionBoth.Valid())
	assert.False(t, Direction("sideways").Valid())
}

func TestDirection_Matches(t *testing.T) {
	assert.True(t, DirectionBoth.Matches(DirectionOutgoing))
	assert.True(t, DirectionBoth.Matches(DirectionIncoming))
	assert.True(t, DirectionOutgoing.Matches(DirectionOutgoing))
	assert.False(t, DirectionOutgoing.Matches(DirectionIncoming))
	assert.False(t, DirectionIncoming.Matches(DirectionOutgoing))
}

func TestSessionStatus_Valid(t *testing.T) {
	assert.True(t, SessionStatusRunning.Valid())
	assert.True(t, SessionStatusCompleted.Valid())
	assert.True(t, SessionStatusCancelled.Valid())
	assert.False(t, SessionStatus("paused").Valid())
}

func TestArenaOutcome_Valid(t *testing.T) {
	assert.True(t, OutcomeSurvived.Valid())
	assert.True(t, OutcomeCrashed.Valid())
	assert.True(t, OutcomeDegraded.Valid())
	assert.False(t, ArenaOutcome("unknown").Valid())
}

func TestAlertStatus_Valid(t *testing.T) {
	assert.True(t, AlertStatusNew.Valid())
	assert.True(t, AlertStatusAcknowledged.Valid())
	assert.True(t, AlertStatusResolved.Valid())
	assert.True(t, AlertStatusFalsePositive.Valid())
	assert.False(t, AlertStatus("pending").Valid())
}

func TestRuleSource_Valid(t *testing.T) {
	assert.True(t, RuleSourceManual.Valid())
	assert.True(t, RuleSourceTemplate.Valid())
	assert.True(t, RuleSourceNaturalLanguage.Valid())
	assert.False(t, RuleSource("ai").Valid())
}

func TestDetectConfig_JSON_SingleString(t *testing.T) {
	raw := `"cross_entity_leak"`
	var dc DetectConfig
	err := json.Unmarshal([]byte(raw), &dc)
	require.NoError(t, err)
	assert.Equal(t, []string{"cross_entity_leak"}, dc.Values)

	// Marshal back: single value → string.
	data, err := json.Marshal(dc)
	require.NoError(t, err)
	assert.Equal(t, `"cross_entity_leak"`, string(data))
}

func TestDetectConfig_JSON_StringSlice(t *testing.T) {
	raw := `["email_address","phone_number","credit_card"]`
	var dc DetectConfig
	err := json.Unmarshal([]byte(raw), &dc)
	require.NoError(t, err)
	assert.Equal(t, []string{"email_address", "phone_number", "credit_card"}, dc.Values)

	data, err := json.Marshal(dc)
	require.NoError(t, err)
	assert.Equal(t, `["email_address","phone_number","credit_card"]`, string(data))
}

func TestDetectConfig_JSON_Null(t *testing.T) {
	raw := `null`
	var dc DetectConfig
	err := json.Unmarshal([]byte(raw), &dc)
	require.NoError(t, err)
	assert.Nil(t, dc.Values)
}

func TestDetectConfig_JSON_Invalid(t *testing.T) {
	raw := `123`
	var dc DetectConfig
	err := json.Unmarshal([]byte(raw), &dc)
	assert.Error(t, err)
}

func TestServer_JSONRoundTrip(t *testing.T) {
	srv := Server{
		ID:   "srv-001",
		Name: "hubspot",
		Mode: ServerModeWrap,
		Args: []string{"@hubspot/mcp-server"},
	}

	data, err := json.Marshal(srv)
	require.NoError(t, err)

	var decoded Server
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, srv.ID, decoded.ID)
	assert.Equal(t, srv.Name, decoded.Name)
	assert.Equal(t, srv.Mode, decoded.Mode)
}

func TestEvent_JSONRoundTrip(t *testing.T) {
	evt := Event{
		ID:          "evt-001",
		ServerID:    "srv-001",
		Direction:   DirectionOutgoing,
		MessageType: "tools/call",
		ToolName:    "search",
		ActionTaken: ActionAllowed,
		LatencyMS:   5,
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var decoded Event
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, evt.ToolName, decoded.ToolName)
	assert.Equal(t, evt.ActionTaken, decoded.ActionTaken)
}

func TestCustomErrors(t *testing.T) {
	pv := &ErrPolicyViolation{Rule: "no-delete", Message: "DELETE blocked", Action: RuleActionBlock}
	assert.Contains(t, pv.Error(), "no-delete")
	assert.Contains(t, pv.Error(), "DELETE blocked")

	pt := &ErrProxyTimeout{ServerName: "postgres", Duration: 30_000_000_000}
	assert.Contains(t, pt.Error(), "postgres")
	assert.Contains(t, pt.Error(), "30s")

	im := &ErrInvalidMessage{Reason: "bad json"}
	assert.Contains(t, im.Error(), "bad json")

	assert.Error(t, ErrTransportClosed)
}
