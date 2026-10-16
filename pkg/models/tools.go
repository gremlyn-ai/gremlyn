package models

type ToolAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty" yaml:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty" yaml:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty" yaml:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty" yaml:"openWorldHint,omitempty"`
}

func (a ToolAnnotations) Declared() bool {
	return a.ReadOnlyHint != nil || a.DestructiveHint != nil || a.IdempotentHint != nil || a.OpenWorldHint != nil
}

func (a ToolAnnotations) Effect() ToolEffect {
	if isTrue(a.ReadOnlyHint) {
		return EffectReadOnly
	}
	destructive := isTrue(a.DestructiveHint)
	nonIdempotent := isFalse(a.IdempotentHint)
	switch {
	case destructive && nonIdempotent:
		return EffectNonIdempotentDestructive
	case destructive:
		return EffectDestructive
	case nonIdempotent:
		return EffectNonIdempotentWrite
	case a.ReadOnlyHint != nil || a.DestructiveHint != nil || a.IdempotentHint != nil:
		return EffectWrite
	default:
		return EffectUnknown
	}
}

func isTrue(b *bool) bool  { return b != nil && *b }
func isFalse(b *bool) bool { return b != nil && !*b }

type AnnotationSource string

const (
	AnnotationSourceServer AnnotationSource = "server"
	AnnotationSourceConfig AnnotationSource = "config"
)

type ToolRecord struct {
	Annotations ToolAnnotations  `json:"annotations"`
	Source      AnnotationSource `json:"source"`
}

type ToolCatalog map[string]ToolRecord

func (c ToolCatalog) Effect(tool string) ToolEffect {
	if tool == "" {
		return EffectUnknown
	}
	rec, ok := c[tool]
	if !ok {
		return EffectUnknown
	}
	return rec.Annotations.Effect()
}

func (c ToolCatalog) Clone() ToolCatalog {
	if len(c) == 0 {
		return nil
	}
	out := make(ToolCatalog, len(c))
	for name, rec := range c {
		out[name] = rec
	}
	return out
}

type ToolEffect string

const (
	EffectUnknown                  ToolEffect = "unknown"
	EffectReadOnly                 ToolEffect = "read_only"
	EffectWrite                    ToolEffect = "write"
	EffectNonIdempotentWrite       ToolEffect = "non_idempotent_write"
	EffectDestructive              ToolEffect = "destructive"
	EffectNonIdempotentDestructive ToolEffect = "non_idempotent_destructive"
)

func AllToolEffects() []ToolEffect {
	return []ToolEffect{
		EffectUnknown,
		EffectReadOnly,
		EffectWrite,
		EffectNonIdempotentWrite,
		EffectDestructive,
		EffectNonIdempotentDestructive,
	}
}

func (e ToolEffect) Known() bool {
	switch e {
	case EffectReadOnly, EffectWrite, EffectNonIdempotentWrite, EffectDestructive, EffectNonIdempotentDestructive:
		return true
	case EffectUnknown:
		return false
	default:
		return false
	}
}

func (e ToolEffect) Destructive() bool {
	return e == EffectDestructive || e == EffectNonIdempotentDestructive
}

func (e ToolEffect) NonIdempotentWrite() bool {
	return e == EffectNonIdempotentWrite || e == EffectNonIdempotentDestructive
}

type EventDetails struct {
	Gremlin       string     `json:"gremlin"`
	Reaction      string     `json:"reaction"`
	Tool          string     `json:"tool"`
	Method        string     `json:"method"`
	NextTool      string     `json:"next_tool,omitempty"`
	ToolSucceeded bool       `json:"tool_succeeded,omitempty"`
	ToolEffect    ToolEffect `json:"tool_effect,omitempty"`
	NextEffect    ToolEffect `json:"next_effect,omitempty"`
	Placement     string     `json:"placement,omitempty"`
}
