// Package gremlins defines the Gremlin interface and concrete chaos failure
// injectors used by Gremlyn Arena to test AI agent resilience.
package gremlins

import (
	"context"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// Gremlin defines the interface that all chaos gremlins must implement.
// Each gremlin injects a specific type of failure into the MCP message flow.
type Gremlin interface {
	// Name returns the gremlin's unique identifier (e.g., "hallucination").
	Name() string

	// Description returns a human-readable description of what this gremlin does.
	Description() string

	// Inject attempts to inject a failure into the given message.
	// Returns the (possibly modified) message, whether injection occurred, and any error.
	Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error)
}
