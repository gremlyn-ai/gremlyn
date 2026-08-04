package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gremlyn-ai/gremlyn/internal/shield/service"
	"github.com/rs/zerolog"
)

// NewRouter creates a chi router with all Shield REST endpoints.
func NewRouter(shield *service.Shield, logger zerolog.Logger) *chi.Mux {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   parseCORSOrigins(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(middleware.RequestID)
	// Deliberately NOT middleware.RealIP. It rewrites r.RemoteAddr from
	// X-Forwarded-For / X-Real-IP / True-Client-IP, which are client-controlled
	// unless a trusted reverse proxy sets them. Gremlyn is local-first and runs
	// with nothing in front of it, so trusting those headers would let a caller
	// forge its own source address in our audit trail — in a security product
	// that is strictly worse than reporting the real socket address.
	// See GHSA-3fxj-6jh8-hvhx, GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf.
	r.Use(middleware.Recoverer)
	r.Use(jsonContentType)

	h := &handlers{shield: shield, logger: logger}

	r.Route("/api/v1", func(r chi.Router) {
		// Shield status.
		r.Get("/status", h.getStatus)
		r.Post("/start", h.postStart)
		r.Post("/stop", h.postStop)

		// Events.
		r.Get("/events", h.listEvents)
		r.Get("/events/blocked", h.listBlockedEvents)

		// Rules.
		r.Get("/rules", h.listRules)
		r.Post("/rules", h.createRule)
		r.Put("/rules/{ruleID}", h.updateRule)
		r.Delete("/rules/{ruleID}", h.deleteRule)

		// Alerts.
		r.Get("/alerts", h.listAlerts)
		r.Patch("/alerts/{alertID}", h.updateAlertStatus)

		// Metrics.
		r.Get("/metrics", h.getMetrics)

		// Terminal exec.
		r.Post("/exec", h.execCommand)

		// Servers.
		r.Get("/servers", h.listServers)
		r.Post("/servers", h.createServer)
		r.Get("/servers/{serverID}", h.getServer)
		r.Put("/servers/{serverID}", h.updateServer)
		r.Delete("/servers/{serverID}", h.deleteServer)
	})

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
