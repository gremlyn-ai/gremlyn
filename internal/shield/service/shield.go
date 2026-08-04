// Package service provides the Shield service orchestration layer.
// It wires the policy engine, detection, storage, and alerting together.
package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/gremlyn-ai/gremlyn/internal/shield/alert"
	"github.com/gremlyn-ai/gremlyn/internal/shield/policy"
	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
)

// EventStore defines the interface for persisting events.
type EventStore interface {
	Insert(ctx context.Context, e *models.Event) (string, error)
	ListByServer(ctx context.Context, serverID string, limit, offset int) ([]models.Event, error)
	ListBlocked(ctx context.Context, limit, offset int) ([]models.Event, error)
	CountByAction(ctx context.Context, hours int) (map[string]int, error)
}

// RuleStore defines the interface for persisting rules.
type RuleStore interface {
	Insert(ctx context.Context, rule *models.Rule) (string, error)
	GetByID(ctx context.Context, id string) (*models.Rule, error)
	ListByServer(ctx context.Context, serverID string) ([]models.Rule, error)
	ListEnabled(ctx context.Context) ([]models.Rule, error)
	Update(ctx context.Context, rule *models.Rule) error
	Delete(ctx context.Context, id string) error
}

// AlertStore defines the interface for persisting alerts.
type AlertStore interface {
	Insert(ctx context.Context, a *models.Alert) (string, error)
	ListRecent(ctx context.Context, limit, offset int) ([]models.Alert, error)
	UpdateStatus(ctx context.Context, id string, status models.AlertStatus) error
	CountBySeverity(ctx context.Context, hours int) (map[string]int, error)
}

// ServerStore defines the interface for persisting server configurations.
type ServerStore interface {
	Insert(ctx context.Context, s *models.Server) (string, error)
	GetByID(ctx context.Context, id string) (*models.Server, error)
	GetByName(ctx context.Context, name string) (*models.Server, error)
	List(ctx context.Context) ([]models.Server, error)
	Update(ctx context.Context, s *models.Server) error
	Delete(ctx context.Context, id string) error
}

// Shield is the main service that orchestrates the firewall components.
type Shield struct {
	cfg        *config.Config
	engines    map[string]*policy.Engine
	pipeline   *proxy.Pipeline
	events     EventStore
	rules      RuleStore
	alerts     AlertStore
	servers    ServerStore
	dispatcher *alert.Dispatcher
	logger     zerolog.Logger
	mu         sync.RWMutex
	running    bool
}

// NewShield creates a new Shield service.
func NewShield(
	cfg *config.Config,
	events EventStore,
	rules RuleStore,
	alerts AlertStore,
	servers ServerStore,
	dispatcher *alert.Dispatcher,
	logger zerolog.Logger,
) *Shield {
	return &Shield{
		cfg:        cfg,
		engines:    make(map[string]*policy.Engine),
		events:     events,
		rules:      rules,
		alerts:     alerts,
		servers:    servers,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// Start initializes the policy engines for each configured server and
// registers them with the pipeline.
func (s *Shield) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("shield is already running")
	}

	s.pipeline = proxy.NewPipeline(s.logger)

	for name, srv := range s.cfg.Servers {
		engine := policy.NewEngine(name, srv.Rules, s.logger,
			policy.WithOnAlert(s.handleAlert),
			policy.WithOnEvent(s.handleEvent),
		)
		s.engines[name] = engine
		s.pipeline.RegisterHandler(engine)
		s.logger.Info().Str("server", name).Int("rules", len(srv.Rules)).Msg("policy engine registered")
	}

	// Register global rate limiter if configured.
	if s.cfg.Global.RateLimit != "" {
		count, window, perAgent, err := config.ParseRateLimit(s.cfg.Global.RateLimit)
		if err != nil {
			s.logger.Warn().Err(err).Msg("invalid rate limit config, skipping")
		} else {
			rl := policy.NewRateLimiter(count, window, perAgent, s.logger)
			s.pipeline.RegisterHandler(rl)
			s.logger.Info().Int("max", count).Dur("window", window).Bool("per_agent", perAgent).Msg("rate limiter registered")
		}
	}

	s.running = true
	s.logger.Info().Msg("shield started")
	return nil
}

// Stop shuts down the shield service.
func (s *Shield) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.engines = make(map[string]*policy.Engine)
	s.logger.Info().Msg("shield stopped")
}

// IsRunning returns whether Shield is currently active.
func (s *Shield) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Pipeline returns the analysis pipeline for use by the proxy.
func (s *Shield) Pipeline() *proxy.Pipeline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pipeline
}

// ServerNames returns the names of all configured MCP servers (config + DB).
func (s *Shield) ServerNames() []string {
	seen := make(map[string]struct{})
	var names []string

	for name := range s.cfg.Servers {
		seen[name] = struct{}{}
		names = append(names, name)
	}

	if s.servers != nil {
		dbServers, err := s.servers.List(context.Background())
		if err == nil {
			for _, srv := range dbServers {
				if _, ok := seen[srv.Name]; !ok {
					names = append(names, srv.Name)
				}
			}
		}
	}

	return names
}

// ConfigServers returns the servers defined in the YAML config.
func (s *Shield) ConfigServers() map[string]config.ServerConfig {
	return s.cfg.Servers
}

// Events returns the event store.
func (s *Shield) Events() EventStore { return s.events }

// Rules returns the rule store.
func (s *Shield) Rules() RuleStore { return s.rules }

// Alerts returns the alert store.
func (s *Shield) Alerts() AlertStore { return s.alerts }

// Servers returns the server store.
func (s *Shield) Servers() ServerStore { return s.servers }

func (s *Shield) handleAlert(ctx context.Context, ae policy.AlertEvent) {
	a := &models.Alert{
		Severity: ae.Severity,
		Type:     ae.Type,
		Message:  ae.Message,
		Status:   models.AlertStatusNew,
	}

	if s.alerts != nil {
		id, err := s.alerts.Insert(ctx, a)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to persist alert")
		} else {
			a.ID = id
		}
	}

	if s.dispatcher != nil {
		event := &models.Event{ToolName: ae.RuleName}
		s.dispatcher.Dispatch(ctx, a, event)
	}
}

func (s *Shield) handleEvent(ctx context.Context, er policy.EventRecord) {
	if s.events == nil {
		return
	}

	e := &models.Event{
		ServerID:         er.ServerID,
		Direction:        er.Direction,
		MessageType:      er.MessageType,
		ToolName:         er.ToolName,
		ToolArgs:         er.ToolArgs,
		ActionTaken:      er.ActionTaken,
		RulesTriggered:   er.RulesTriggered,
		DetectionResults: er.DetectionResults,
		LatencyMS:        er.LatencyMS,
	}

	if _, err := s.events.Insert(ctx, e); err != nil {
		s.logger.Error().Err(err).Msg("failed to persist event")
	}
}
