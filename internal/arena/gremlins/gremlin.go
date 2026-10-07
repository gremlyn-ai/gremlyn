package gremlins

import (
	"context"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type Gremlin interface {
	Name() string
	Description() string
	Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error)
}
