package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	assert.False(t, SessionStatus("cancelled").Valid())
	assert.False(t, SessionStatus("paused").Valid())
}

func TestArenaOutcome_Valid(t *testing.T) {
	assert.True(t, OutcomeSurvived.Valid())
	assert.True(t, OutcomeCrashed.Valid())
	assert.True(t, OutcomeDegraded.Valid())
	assert.False(t, ArenaOutcome("unknown").Valid())
}
