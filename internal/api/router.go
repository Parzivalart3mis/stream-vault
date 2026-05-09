package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/yashk/streamvault/internal/leaderboard"
	"github.com/yashk/streamvault/internal/processor"
	"github.com/yashk/streamvault/internal/store"
)

func NewRouter(s *store.Store, p *processor.Pipeline, lb *leaderboard.Cache, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	h := NewHandlers(s, p, lb, log)

	r.Use(chimiddleware.RealIP)
	r.Use(RequestID)
	r.Use(Logger(log))
	r.Use(Recovery(log))

	r.Get("/health", h.Health)

	r.Route("/creators", func(r chi.Router) {
		r.Post("/", h.CreateCreator)
		r.Route("/{id}", func(r chi.Router) {
			r.Post("/subscribe", h.Subscribe)
			r.Post("/gift", h.Gift)
			r.Post("/tip", h.Tip)
			r.Get("/revenue", h.Revenue)
			r.Get("/events", h.Events)
		})
	})

	r.Get("/leaderboard", h.Leaderboard)

	return r
}
