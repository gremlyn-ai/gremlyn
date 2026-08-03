// Package alert provides notification delivery for Gremlyn Shield alerts.
// Supports Slack webhooks, generic webhooks, and extensible channel types.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

// Notifier sends alert notifications to an external channel.
type Notifier interface {
	// Send delivers an alert notification. Returns an error if delivery fails.
	Send(ctx context.Context, alert *models.Alert, event *models.Event) error
	// Type returns the channel type name (e.g., "slack", "webhook").
	Type() string
}

// SlackNotifier sends alerts to a Slack channel via incoming webhook.
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
	logger     zerolog.Logger
}

// NewSlackNotifier creates a new SlackNotifier with the given webhook URL.
func NewSlackNotifier(webhookURL string, logger zerolog.Logger) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

type slackMessage struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks,omitempty"`
}

type slackBlock struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Send delivers the alert to Slack via webhook.
func (s *SlackNotifier) Send(ctx context.Context, alert *models.Alert, event *models.Event) error {
	emoji := severityEmoji(alert.Severity)
	title := fmt.Sprintf("%s Gremlyn Shield Alert — %s", emoji, alert.Type)

	detail := fmt.Sprintf("*Severity:* %s\n*Message:* %s\n*Server:* %s\n*Direction:* %s",
		alert.Severity, alert.Message, event.ServerID, event.Direction)
	if event.ToolName != "" {
		detail += fmt.Sprintf("\n*Tool:* %s", event.ToolName)
	}

	msg := slackMessage{
		Text: title,
		Blocks: []slackBlock{
			{Type: "header", Text: &slackText{Type: "plain_text", Text: title}},
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: detail}},
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending slack webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	s.logger.Info().Str("alert_id", alert.ID).Msg("slack alert sent")
	return nil
}

// Type returns "slack".
func (s *SlackNotifier) Type() string { return "slack" }

// WebhookNotifier sends alerts to a generic HTTP webhook endpoint.
type WebhookNotifier struct {
	url    string
	client *http.Client
	logger zerolog.Logger
}

// NewWebhookNotifier creates a new WebhookNotifier.
func NewWebhookNotifier(url string, logger zerolog.Logger) *WebhookNotifier {
	return &WebhookNotifier{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

type webhookPayload struct {
	Alert *models.Alert `json:"alert"`
	Event *models.Event `json:"event"`
}

// Send delivers the alert to the webhook endpoint.
func (w *WebhookNotifier) Send(ctx context.Context, alert *models.Alert, event *models.Event) error {
	payload := webhookPayload{Alert: alert, Event: event}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	w.logger.Info().Str("alert_id", alert.ID).Msg("webhook alert sent")
	return nil
}

// Type returns "webhook".
func (w *WebhookNotifier) Type() string { return "webhook" }

// Dispatcher routes alerts to the appropriate notifiers based on configuration.
type Dispatcher struct {
	notifiers []Notifier
	logger    zerolog.Logger
}

// NewDispatcher creates a new Dispatcher with the given notifiers.
func NewDispatcher(notifiers []Notifier, logger zerolog.Logger) *Dispatcher {
	return &Dispatcher{
		notifiers: notifiers,
		logger:    logger,
	}
}

// Dispatch sends the alert to all registered notifiers.
// Errors are logged but do not stop delivery to other channels.
func (d *Dispatcher) Dispatch(ctx context.Context, alert *models.Alert, event *models.Event) {
	for _, n := range d.notifiers {
		if err := n.Send(ctx, alert, event); err != nil {
			d.logger.Error().Err(err).
				Str("channel", n.Type()).
				Str("alert_id", alert.ID).
				Msg("failed to send alert")
		}
	}
}

func severityEmoji(s models.AlertSeverity) string {
	switch s {
	case models.AlertSeverityCritical:
		return "\xF0\x9F\x94\xB4" // red circle
	case models.AlertSeverityHigh:
		return "\xF0\x9F\x9F\xA0" // orange circle
	case models.AlertSeverityMedium:
		return "\xF0\x9F\x9F\xA1" // yellow circle
	case models.AlertSeverityLow:
		return "\xF0\x9F\x94\xB5" // blue circle
	default:
		return "\xE2\x9A\xA0\xEF\xB8\x8F" // warning
	}
}
