package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gremlyn-ai/gremlyn/internal/arena/service"
	"github.com/rs/zerolog"
)

// NewRouter creates the chi router with all Arena API endpoints.
func NewRouter(arena *service.Arena, hub *Hub, logger zerolog.Logger) *chi.Mux {
	h := &handlers{arena: arena, hub: hub}

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   parseCORSOrigins(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(middleware.Recoverer)
	// Deliberately NOT middleware.RealIP — see the note in
	// internal/shield/api/router.go. Client-controlled forwarding headers must
	// not overwrite the real socket address.
	r.Use(jsonContentType)

	r.Route("/arena", func(r chi.Router) {
		// Sessions
		r.Post("/sessions", h.createSession)
		r.Get("/sessions", h.listSessions)
		r.Get("/sessions/{id}", h.getSession)
		r.Post("/sessions/{id}/stop", h.stopSession)
		r.Get("/sessions/{id}/events", h.getSessionEvents)

		// WebSocket for live session streaming
		r.Get("/sessions/{id}/ws", h.sessionWS)

		// Gremlins
		r.Get("/gremlins", h.listGremlins)
	})

	// Health check
	r.Get("/status", h.getStatus)

	logger.Info().Msg("arena API routes registered")
	return r
}

// parseCORSOrigins reads allowed origins from CORS_ALLOWED_ORIGINS env var.
// Falls back to localhost defaults for development.
func parseCORSOrigins() []string {
	origins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if origins == "" {
		return []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	}
	return strings.Split(origins, ",")
}

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}
