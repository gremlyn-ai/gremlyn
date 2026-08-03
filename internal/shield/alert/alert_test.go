package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlackNotifier_Send(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&receivedBody))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL, zerolog.Nop())
	assert.Equal(t, "slack", notifier.Type())

	testAlert := &models.Alert{
		ID:       "alert-1",
		Severity: models.AlertSeverityHigh,
		Type:     "prompt_injection",
		Message:  "Injection detected",
	}
	testEvent := &models.Event{
		ServerID:  "hubspot",
		Direction: models.DirectionIncoming,
		ToolName:  "search",
	}

	err := notifier.Send(context.Background(), testAlert, testEvent)
	require.NoError(t, err)
	assert.Contains(t, receivedBody["text"].(string), "Gremlyn Shield Alert")
}

func TestSlackNotifier_SendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL, zerolog.Nop())
	err := notifier.Send(context.Background(), &models.Alert{Severity: models.AlertSeverityLow}, &models.Event{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestWebhookNotifier_Send(t *testing.T) {
	var receivedPayload webhookPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&receivedPayload))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.URL, zerolog.Nop())
	assert.Equal(t, "webhook", notifier.Type())

	testAlert := &models.Alert{ID: "a1", Severity: models.AlertSeverityMedium, Message: "test"}
	testEvent := &models.Event{ServerID: "srv1"}

	err := notifier.Send(context.Background(), testAlert, testEvent)
	require.NoError(t, err)
	assert.Equal(t, "a1", receivedPayload.Alert.ID)
	assert.Equal(t, "srv1", receivedPayload.Event.ServerID)
}

func TestDispatcher_DispatchesToAll(t *testing.T) {
	var count int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifiers := []Notifier{
		NewSlackNotifier(server.URL, zerolog.Nop()),
		NewWebhookNotifier(server.URL, zerolog.Nop()),
	}
	dispatcher := NewDispatcher(notifiers, zerolog.Nop())

	dispatcher.Dispatch(context.Background(),
		&models.Alert{Severity: models.AlertSeverityLow, Message: "test"},
		&models.Event{},
	)
	assert.Equal(t, 2, count, "dispatcher should call all notifiers")
}

func TestSeverityEmoji(t *testing.T) {
	// Smoke test — just ensure it returns something for each severity.
	assert.NotEmpty(t, severityEmoji(models.AlertSeverityCritical))
	assert.NotEmpty(t, severityEmoji(models.AlertSeverityHigh))
	assert.NotEmpty(t, severityEmoji(models.AlertSeverityMedium))
	assert.NotEmpty(t, severityEmoji(models.AlertSeverityLow))
	assert.NotEmpty(t, severityEmoji("unknown"))
}
