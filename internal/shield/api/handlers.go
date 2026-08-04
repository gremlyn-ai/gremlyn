package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gremlyn-ai/gremlyn/internal/shield/service"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

type handlers struct {
	shield *service.Shield
	logger zerolog.Logger
}

func (h *handlers) getStatus(w http.ResponseWriter, _ *http.Request) {
	resp := StatusResponse{
		Running: h.shield.IsRunning(),
		Servers: h.shield.ServerNames(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) postStart(w http.ResponseWriter, r *http.Request) {
	if err := h.shield.Start(r.Context()); err != nil {
		writeError(w, http.StatusConflict, err.Error(), "ALREADY_RUNNING")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (h *handlers) postStop(w http.ResponseWriter, _ *http.Request) {
	h.shield.Stop()
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (h *handlers) listEvents(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	limit, offset := parsePagination(r)

	events, err := h.shield.Events().ListByServer(r.Context(), serverID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, EventListResponse{Events: events, Total: len(events)})
}

func (h *handlers) listBlockedEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	events, err := h.shield.Events().ListBlocked(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, EventListResponse{Events: events, Total: len(events)})
}

func (h *handlers) listRules(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	var rules []models.Rule
	var err error

	if serverID != "" {
		rules, err = h.shield.Rules().ListByServer(r.Context(), serverID)
	} else {
		rules, err = h.shield.Rules().ListEnabled(r.Context())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, RuleListResponse{Rules: rules})
}

func (h *handlers) createRule(w http.ResponseWriter, r *http.Request) {
	var req CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if req.Name == "" || !req.Action.Valid() {
		writeError(w, http.StatusBadRequest, "name and valid action are required", "VALIDATION_ERROR")
		return
	}

	rule := &models.Rule{
		ServerID:      req.ServerID,
		Name:          req.Name,
		ScanResponses: req.ScanResponses,
		ScanOutgoing:  req.ScanOutgoing,
		Action:        req.Action,
		Enabled:       true,
		Source:        models.RuleSourceManual,
	}

	if req.MatchTool != "" {
		rule.Match = &models.RuleMatch{Tool: req.MatchTool}
	}
	if len(req.Detect) > 0 {
		rule.Detect = models.DetectConfig{Values: req.Detect}
	}

	id, err := h.shield.Rules().Insert(r.Context(), rule)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	rule.ID = id
	writeJSON(w, http.StatusCreated, rule)
}

func (h *handlers) updateRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleID")

	rule, err := h.shield.Rules().GetByID(r.Context(), ruleID)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found", "NOT_FOUND")
		return
	}

	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}

	if err := h.shield.Rules().Update(r.Context(), rule); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *handlers) deleteRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleID")
	if err := h.shield.Rules().Delete(r.Context(), ruleID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": ruleID})
}

func (h *handlers) listAlerts(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	alerts, err := h.shield.Alerts().ListRecent(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, AlertListResponse{Alerts: alerts})
}

func (h *handlers) updateAlertStatus(w http.ResponseWriter, r *http.Request) {
	alertID := chi.URLParam(r, "alertID")

	var req UpdateAlertStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if !req.Status.Valid() {
		writeError(w, http.StatusBadRequest, "invalid alert status", "VALIDATION_ERROR")
		return
	}

	if err := h.shield.Alerts().UpdateStatus(r.Context(), alertID, req.Status); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": alertID})
}

func (h *handlers) getMetrics(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v > 0 {
			hours = v
		}
	}

	eventCounts, err := h.shield.Events().CountByAction(r.Context(), hours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	alertCounts, err := h.shield.Alerts().CountBySeverity(r.Context(), hours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, MetricsResponse{
		EventCounts: eventCounts,
		AlertCounts: alertCounts,
		PeriodHours: hours,
	})
}

// ── Server handlers ──

func (h *handlers) listServers(w http.ResponseWriter, r *http.Request) {
	store := h.shield.Servers()
	if store == nil {
		writeJSON(w, http.StatusOK, ServerListResponse{Servers: []ServerResponse{}})
		return
	}

	servers, err := store.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	resp := ServerListResponse{Servers: make([]ServerResponse, 0, len(servers))}
	for _, s := range servers {
		resp.Servers = append(resp.Servers, serverToResponse(s, "api"))
	}

	// Also include servers from YAML config that aren't in DB.
	dbNames := make(map[string]struct{}, len(servers))
	for _, s := range servers {
		dbNames[s.Name] = struct{}{}
	}
	for name := range h.shield.ConfigServers() {
		if _, exists := dbNames[name]; !exists {
			resp.Servers = append(resp.Servers, ServerResponse{
				Name:   name,
				Mode:   "proxy",
				Status: "active",
				Source: "yaml",
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) getServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	store := h.shield.Servers()
	if store == nil {
		writeError(w, http.StatusNotFound, "server not found", "NOT_FOUND")
		return
	}

	s, err := store.GetByID(r.Context(), serverID)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found", "NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, serverToResponse(*s, "api"))
}

func (h *handlers) createServer(w http.ResponseWriter, r *http.Request) {
	store := h.shield.Servers()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "server store unavailable", "NO_STORE")
		return
	}

	var req CreateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "VALIDATION_ERROR")
		return
	}
	if req.Mode == "" {
		req.Mode = "proxy"
	}

	s := &models.Server{
		Name:        req.Name,
		Mode:        models.ServerMode(req.Mode),
		UpstreamURL: req.UpstreamURL,
		Command:     req.Command,
		Args:        req.Args,
		Env:         req.Env,
		AuthHeader:  req.AuthHeader,
		Headers:     req.Headers,
	}

	id, err := store.Insert(r.Context(), s)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error(), "INSERT_FAILED")
		return
	}
	s.ID = id
	writeJSON(w, http.StatusCreated, serverToResponse(*s, "api"))
}

func (h *handlers) updateServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	store := h.shield.Servers()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "server store unavailable", "NO_STORE")
		return
	}

	s, err := store.GetByID(r.Context(), serverID)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found", "NOT_FOUND")
		return
	}

	var req UpdateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if req.Name != nil {
		s.Name = *req.Name
	}
	if req.Mode != nil {
		s.Mode = models.ServerMode(*req.Mode)
	}
	if req.UpstreamURL != nil {
		s.UpstreamURL = *req.UpstreamURL
	}
	if req.Command != nil {
		s.Command = *req.Command
	}
	if req.Args != nil {
		s.Args = req.Args
	}
	if req.Env != nil {
		s.Env = req.Env
	}
	if req.AuthHeader != nil {
		s.AuthHeader = *req.AuthHeader
	}
	if req.Headers != nil {
		s.Headers = req.Headers
	}
	if req.Status != nil {
		s.Status = *req.Status
	}

	if err := store.Update(r.Context(), s); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, serverToResponse(*s, "api"))
}

func (h *handlers) deleteServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	store := h.shield.Servers()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "server store unavailable", "NO_STORE")
		return
	}

	if err := store.Delete(r.Context(), serverID); err != nil {
		writeError(w, http.StatusNotFound, err.Error(), "NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": serverID})
}

func serverToResponse(s models.Server, source string) ServerResponse {
	// Mask auth header — only indicate presence, don't expose the value.
	maskedAuth := ""
	if s.AuthHeader != "" {
		maskedAuth = "********"
	}
	return ServerResponse{
		ID:          s.ID,
		Name:        s.Name,
		Mode:        string(s.Mode),
		UpstreamURL: s.UpstreamURL,
		Command:     s.Command,
		Args:        s.Args,
		Env:         s.Env,
		AuthHeader:  maskedAuth,
		Headers:     s.Headers,
		Status:      s.Status,
		Source:      source,
		CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// ── Terminal exec ──

func (h *handlers) execCommand(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		writeJSON(w, http.StatusOK, ExecResponse{Output: "", Status: 0})
		return
	}

	parts := splitCommand(cmd)
	if len(parts) == 0 {
		writeJSON(w, http.StatusOK, ExecResponse{Output: "", Status: 0})
		return
	}

	out, execErr := h.dispatchCommand(r.Context(), parts)
	resp := ExecResponse{Output: out, Status: 0}
	if execErr != nil {
		resp.Error = execErr.Error()
		resp.Status = 1
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) dispatchCommand(ctx context.Context, parts []string) (string, error) {
	switch parts[0] {
	case "help":
		return `GREMLYN TERMINAL beta
Available commands:
  help                    Show this help
  version                 Show version info
  status                  Show shield status
  shield status           Shield running state
  shield rules            List enforcement rules
  shield events [--limit] Recent shield events
  arena status            Arena service status
  arena sessions          List arena sessions
  arena gremlins          List available gremlins
  clear                   Clear terminal (client-side)`, nil

	case "version":
		return "gremlyn beta", nil

	case "status", "shield":
		return h.execShield(ctx, parts)

	case "arena":
		return h.execArena(ctx, parts[1:])

	default:
		return "", fmt.Errorf("unknown command: %s. Type 'help' for available commands", parts[0])
	}
}

func (h *handlers) execShield(ctx context.Context, parts []string) (string, error) {
	// "status" as top-level or "shield status"
	sub := "status"
	if parts[0] == "shield" && len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "status":
		running := h.shield.IsRunning()
		servers := h.shield.ServerNames()
		state := "STOPPED"
		if running {
			state = "RUNNING"
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Shield: %s\n", state))
		sb.WriteString(fmt.Sprintf("Servers: %d\n", len(servers)))
		for _, s := range servers {
			sb.WriteString(fmt.Sprintf("  - %s\n", s))
		}
		return strings.TrimRight(sb.String(), "\n"), nil

	case "rules":
		rules, err := h.shield.Rules().ListEnabled(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to list rules: %w", err)
		}
		if len(rules) == 0 {
			return "No rules configured.", nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%-8s %-24s %-16s %-8s\n", "ID", "NAME", "ACTION", "ENABLED"))
		sb.WriteString(strings.Repeat("-", 60) + "\n")
		for _, r := range rules {
			id := r.ID
			if len(id) > 8 {
				id = id[:8]
			}
			sb.WriteString(fmt.Sprintf("%-8s %-24s %-16s %-8v\n", id, r.Name, r.Action, r.Enabled))
		}
		return strings.TrimRight(sb.String(), "\n"), nil

	case "events":
		limit := 20
		if len(parts) > 2 {
			for i, p := range parts[2:] {
				if p == "--limit" && i+1 < len(parts[2:]) {
					if n, err := strconv.Atoi(parts[i+3]); err == nil && n > 0 {
						limit = n
					}
				}
			}
		}
		events, err := h.shield.Events().ListBlocked(ctx, limit, 0)
		if err != nil {
			return "", fmt.Errorf("failed to list events: %w", err)
		}
		if len(events) == 0 {
			return "No events recorded.", nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%-20s %-16s %-12s %-10s\n", "TIME", "TOOL", "ACTION", "SERVER"))
		sb.WriteString(strings.Repeat("-", 62) + "\n")
		for _, e := range events {
			ts := e.Timestamp.Format("15:04:05")
			sb.WriteString(fmt.Sprintf("%-20s %-16s %-12s %-10s\n", ts, e.ToolName, e.ActionTaken, e.ServerID))
		}
		return strings.TrimRight(sb.String(), "\n"), nil

	default:
		return "", fmt.Errorf("unknown shield command: %s", sub)
	}
}

func (h *handlers) execArena(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: arena <status|sessions|gremlins>")
	}

	arenaURL := os.Getenv("ARENA_URL")
	if arenaURL == "" {
		arenaURL = "http://localhost:8082"
	}

	switch args[0] {
	case "status":
		body, err := arenaHTTP(ctx, arenaURL+"/arena/status")
		if err != nil {
			return "", fmt.Errorf("arena unreachable: %w", err)
		}
		var status struct {
			ActiveSessions    int `json:"active_sessions"`
			GremlinsAvailable int `json:"gremlins_available"`
		}
		if err := json.Unmarshal(body, &status); err != nil {
			return string(body), nil
		}
		return fmt.Sprintf("Arena: ONLINE\nActive sessions: %d\nGremlins available: %d",
			status.ActiveSessions, status.GremlinsAvailable), nil

	case "sessions":
		body, err := arenaHTTP(ctx, arenaURL+"/arena/sessions")
		if err != nil {
			return "", fmt.Errorf("arena unreachable: %w", err)
		}
		var resp struct {
			Sessions []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Server string `json:"server_id"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return string(body), nil
		}
		if len(resp.Sessions) == 0 {
			return "No arena sessions.", nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%-10s %-12s %-16s\n", "ID", "STATUS", "SERVER"))
		sb.WriteString(strings.Repeat("-", 40) + "\n")
		for _, s := range resp.Sessions {
			id := s.ID
			if len(id) > 8 {
				id = id[:8]
			}
			sb.WriteString(fmt.Sprintf("%-10s %-12s %-16s\n", id, s.Status, s.Server))
		}
		return strings.TrimRight(sb.String(), "\n"), nil

	case "gremlins":
		body, err := arenaHTTP(ctx, arenaURL+"/arena/gremlins")
		if err != nil {
			return "", fmt.Errorf("arena unreachable: %w", err)
		}
		var resp struct {
			Gremlins []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"gremlins"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return string(body), nil
		}
		if len(resp.Gremlins) == 0 {
			return "No gremlins available.", nil
		}
		var sb strings.Builder
		for _, g := range resp.Gremlins {
			sb.WriteString(fmt.Sprintf("  %-16s %s\n", g.Name, g.Description))
		}
		return strings.TrimRight(sb.String(), "\n"), nil

	default:
		return "", fmt.Errorf("unknown arena command: %s", args[0])
	}
}

func arenaHTTP(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	return io.ReadAll(resp.Body)
}

func splitCommand(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(ch)
			}
		} else if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
		} else if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg, Code: code})
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}
