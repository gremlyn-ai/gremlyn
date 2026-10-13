package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
)

type chaosSetup struct {
	Gremlins      []string
	Seed          int64
	Intensity     string
	Params        chaos.GremlinParams
	Window        time.Duration
	EventsPath    string
	SummaryPath   string
	ServerCommand []string
}

type chaosRun struct {
	setup     chaosSetup
	pipeline  *proxy.Pipeline
	observer  *chaos.Observer
	eventSink *chaos.FileEventSink
	session   *chaosSessionRecorder
	logger    zerolog.Logger
}

func newChaosRun(ctx context.Context, setup chaosSetup, logger zerolog.Logger) (*chaosRun, error) {
	r := &chaosRun{setup: setup, pipeline: proxy.NewPipeline(logger), logger: logger}
	if len(setup.Gremlins) == 0 {
		return r, nil
	}

	gs, err := chaos.BuildGremlinsWithParams(setup.Gremlins, setup.Seed, chaos.Intensity(setup.Intensity), setup.Params)
	if err != nil {
		r.close()
		return nil, fmt.Errorf("configuring chaos: %w", err)
	}

	var sinks []chaos.EventSink
	if setup.EventsPath != "" {
		r.eventSink, err = chaos.NewFileEventSink(setup.EventsPath)
		if err != nil {
			r.close()
			return nil, fmt.Errorf("configuring chaos events: %w", err)
		}
		sinks = append(sinks, r.eventSink)
	}

	r.session = newChaosSessionRecorder(ctx, chaosRecipe{
		Gremlins:  setup.Gremlins,
		Seed:      setup.Seed,
		Intensity: setup.Intensity,
		Params:    setup.Params,
		Server:    setup.ServerCommand,
	}, logger)
	if r.session != nil {
		sinks = append(sinks, r.session)
	}

	window := setup.Window
	if window <= 0 {
		window = chaos.DefaultWindow
	}
	r.observer = chaos.NewObserver(combineEventSinks(sinks), logger, chaos.WithWindow(window))
	r.pipeline.RegisterHandler(chaos.NewGremlinHandler(gs, r.observer, logger))
	r.pipeline.RegisterHandler(r.observer)

	logger.Info().
		Strs("gremlins", setup.Gremlins).
		Int64("seed", setup.Seed).
		Str("intensity", setup.Intensity).
		Msg("chaos mode enabled")
	return r, nil
}

func (r *chaosRun) finish(ctx context.Context, runErr error, ended bool) {
	if r.observer == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	cov := r.observer.Finalize(ctx, ended)
	r.logger.Info().
		Int("injected", cov.Injected).
		Int("observed", cov.Observed).
		Int("unresolved", cov.Unresolved).
		Bool("complete", cov.Complete()).
		Msg("chaos session finished")

	if r.setup.SummaryPath != "" {
		summary := chaos.SessionSummary{
			Coverage:      cov,
			Gremlins:      r.setup.Gremlins,
			Seed:          r.setup.Seed,
			Intensity:     r.setup.Intensity,
			Params:        r.setup.Params,
			ServerCommand: r.setup.ServerCommand,
		}
		if err := chaos.WriteSummary(r.setup.SummaryPath, summary); err != nil {
			r.logger.Warn().Err(err).Msg("writing chaos summary")
		}
	}

	if r.session != nil {
		r.session.finish(ctx, runErr)
	}
}

func (r *chaosRun) close() {
	r.pipeline.Close()
	if r.eventSink != nil {
		if err := r.eventSink.Close(); err != nil {
			r.logger.Warn().Err(err).Msg("closing chaos event file")
		}
	}
}
