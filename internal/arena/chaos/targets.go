package chaos

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

type ListingState string

const (
	ListingListed     ListingState = "listed"
	ListingNotListed  ListingState = "not_listed"
	ListingUnverified ListingState = "unverified"
)

type TargetState string

const (
	TargetNeverCalled TargetState = "never_called"
	TargetNotInjected TargetState = "not_injected"
	TargetUnobserved  TargetState = "unobserved"
	TargetScored      TargetState = "scored"
)

type TargetStat struct {
	Name     string       `json:"name"`
	Called   int          `json:"called"`
	Injected int          `json:"injected"`
	Listing  ListingState `json:"listing"`
	ListedAs string       `json:"listed_as,omitempty"`
}

type TargetResult struct {
	TargetStat
	Scored int         `json:"scored"`
	State  TargetState `json:"state"`
}

func ResolveTargets(stats []TargetStat, events []models.ArenaEvent) []TargetResult {
	if stats == nil {
		return nil
	}
	scored := map[string]int{}
	for i := range events {
		var d models.EventDetails
		if events[i].Outcome != models.OutcomeUnmeasured && json.Unmarshal(events[i].Details, &d) == nil {
			scored[d.Tool]++
		}
	}
	out := make([]TargetResult, len(stats))
	for i, s := range stats {
		r := TargetResult{TargetStat: s, Scored: scored[s.Name]}
		switch {
		case s.Called == 0:
			r.State = TargetNeverCalled
		case s.Injected == 0:
			r.State = TargetNotInjected
		case r.Scored == 0:
			r.State = TargetUnobserved
		default:
			r.State = TargetScored
		}
		out[i] = r
	}
	return out
}

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
