package chaos

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
)

func ValidateTargets(targets []string, gs []gremlins.Gremlin) error {
	if len(targets) == 0 {
		return errors.New("the list is empty: name at least one tool, or remove the key")
	}
	for i, t := range targets {
		switch {
		case strings.TrimSpace(t) == "":
			return fmt.Errorf("entry %d is empty", i+1)
		case strings.TrimSpace(t) != t:
			return fmt.Errorf("%q has leading or trailing whitespace: a target is the exact name the server lists", t)
		case slices.Contains(targets[:i], t):
			return fmt.Errorf("%q is listed twice", t)
		}
		for _, g := range gs {
			if h, ok := g.(*gremlins.HallucinationGremlin); ok && h.FakeToolName == t {
				return fmt.Errorf("%q is the tool the hallucination gremlin invents, so no real call to it exists", t)
			}
		}
	}
	return nil
}
