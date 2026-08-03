package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gremlyn-ai/gremlyn/internal/arena/service"
	"github.com/gremlyn-ai/gremlyn/internal/arena/session"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

type handlers struct {
	arena *service.Arena
	hub   *Hub
}

func (h *handlers) createSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "INVALID_BODY")
		return
	}

	if req.ServerID == "" {
		writeError(w, http.StatusBadRequest, "server_id is required", "MISSING_FIELD")
		return
	}
	if len(req.Gremlins) == 0 {
		writeError(w, http.StatusBadRequest, "at least one gremlin is required", "MISSING_FIELD")
		return
	}

	cfg := session.Config{
		Gremlins:  req.Gremlins,
		Intensity: req.Intensity,
		Prompts:   req.Prompts,
	}

	sess, err := h.arena.StartSession(r.Context(), req.ServerID, cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "SESSION_START_FAILED")
		return
	}

	writeJSON(w, http.StatusCreated, sessionToResponse(sess.Model()))
}

func (h *handlers) listSessions(w http.ResponseWriter, _ *http.Request) {
	sessions := h.arena.ListSessions()
	resp := SessionListResponse{Sessions: make([]SessionResponse, 0, len(sessions))}
	for _, s := range sessions {
		resp.Sessions = append(resp.Sessions, sessionToResponse(s.Model()))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := h.arena.GetSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, sessionToResponse(sess.Model()))
}

func (h *handlers) stopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.arena.StopSession(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "STOP_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *handlers) getSessionEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	events, err := h.arena.GetSessionEvents(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
		return
	}

	resp := EventListResponse{Events: make([]EventResponse, 0, len(events))}
	for _, e := range events {
		resp.Events = append(resp.Events, eventToResponse(e))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) listGremlins(w http.ResponseWriter, _ *http.Request) {
	gs := h.arena.ListGremlins()
	resp := GremlinListResponse{Gremlins: make([]GremlinInfo, 0, len(gs))}
	for _, g := range gs {
		resp.Gremlins = append(resp.Gremlins, GremlinInfo{
			Name:        g.Name(),
			Description: g.Description(),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) getStatus(w http.ResponseWriter, _ *http.Request) {
	sessions := h.arena.ListSessions()
	active := 0
	for _, s := range sessions {
		if s.Status() == models.SessionStatusRunning {
			active++
		}
	}
	writeJSON(w, http.StatusOK, StatusResponse{
		Status:         "ok",
		ActiveSessions: active,
		Gremlins:       len(h.arena.ListGremlins()),
	})
}

func (h *handlers) sessionWS(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := h.arena.GetSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
		return
	}
	h.hub.HandleWS(w, r, id)
}

// ── JSON helpers ──

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	writeJSON(w, status, ErrorResponse{Error: msg, Code: code})
}
