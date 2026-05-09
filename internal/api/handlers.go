package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/yashk/streamvault/internal/domain"
	"github.com/yashk/streamvault/internal/leaderboard"
	"github.com/yashk/streamvault/internal/processor"
	"github.com/yashk/streamvault/internal/store"
)

type Handlers struct {
	store       *store.Store
	pipeline    *processor.Pipeline
	leaderboard *leaderboard.Cache
	validate    *validator.Validate
	log         *slog.Logger
	startTime   time.Time
}

func NewHandlers(s *store.Store, p *processor.Pipeline, lb *leaderboard.Cache, log *slog.Logger) *Handlers {
	return &Handlers{
		store:       s,
		pipeline:    p,
		leaderboard: lb,
		validate:    validator.New(),
		log:         log,
		startTime:   time.Now(),
	}
}

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	rid := requestIDFromCtx(r.Context())

	dbStatus := "ok"
	if err := h.store.Ping(r.Context()); err != nil {
		h.log.WarnContext(r.Context(), "db ping failed", slog.String("error", err.Error()))
		dbStatus = "error"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"db":     dbStatus,
		"uptime": time.Since(h.startTime).String(),
	}, rid)
}

// POST /creators
type createCreatorRequest struct {
	DisplayName string `json:"display_name" validate:"required,min=1,max=100"`
}

func (h *Handlers) CreateCreator(w http.ResponseWriter, r *http.Request) {
	rid := requestIDFromCtx(r.Context())

	var req createCreatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is not valid JSON", rid)
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error(), rid)
		return
	}

	c := &domain.Creator{
		ID:          uuid.New(),
		DisplayName: req.DisplayName,
		CreatedAt:   time.Now().UTC(),
	}
	if err := h.store.CreateCreator(r.Context(), c); err != nil {
		h.log.ErrorContext(r.Context(), "create creator", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create creator", rid)
		return
	}

	writeJSON(w, http.StatusCreated, c, rid)
}

// POST /creators/{id}/subscribe
type subscribeRequest struct {
	Tier int `json:"tier" validate:"required,min=1,max=3"`
}

func (h *Handlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	h.submitEvent(w, r, domain.EventSubscription, func(req map[string]json.RawMessage) (*eventParams, error) {
		var body subscribeRequest
		if data, ok := req["_body"]; ok {
			if err := json.Unmarshal(data, &body); err != nil {
				return nil, errBadJSON
			}
		}
		if err := h.validate.Struct(body); err != nil {
			return nil, &validationErr{err.Error()}
		}
		cents := domain.TierCents[body.Tier]
		return &eventParams{tier: &body.Tier, quantity: 1, amountCents: cents}, nil
	})
}

// POST /creators/{id}/gift
type giftRequest struct {
	Quantity int `json:"quantity" validate:"required,min=1,max=100"`
	Tier     int `json:"tier" validate:"omitempty,min=1,max=3"`
}

func (h *Handlers) Gift(w http.ResponseWriter, r *http.Request) {
	h.submitEvent(w, r, domain.EventGift, func(req map[string]json.RawMessage) (*eventParams, error) {
		var body giftRequest
		if data, ok := req["_body"]; ok {
			if err := json.Unmarshal(data, &body); err != nil {
				return nil, errBadJSON
			}
		}
		if err := h.validate.Struct(body); err != nil {
			return nil, &validationErr{err.Error()}
		}
		tier := 1
		if body.Tier != 0 {
			tier = body.Tier
		}
		cents := domain.TierCents[tier] * int64(body.Quantity)
		return &eventParams{tier: &tier, quantity: body.Quantity, amountCents: cents}, nil
	})
}

// POST /creators/{id}/tip
type tipRequest struct {
	AmountCents int64 `json:"amount_cents" validate:"required,min=1,max=100000"`
}

func (h *Handlers) Tip(w http.ResponseWriter, r *http.Request) {
	h.submitEvent(w, r, domain.EventTip, func(req map[string]json.RawMessage) (*eventParams, error) {
		var body tipRequest
		if data, ok := req["_body"]; ok {
			if err := json.Unmarshal(data, &body); err != nil {
				return nil, errBadJSON
			}
		}
		if err := h.validate.Struct(body); err != nil {
			return nil, &validationErr{err.Error()}
		}
		return &eventParams{tier: nil, quantity: 1, amountCents: body.AmountCents}, nil
	})
}

type eventParams struct {
	tier        *int
	quantity    int
	amountCents int64
}

var errBadJSON = errors.New("invalid json")

type validationErr struct{ msg string }

func (e *validationErr) Error() string { return e.msg }

func (h *Handlers) submitEvent(
	w http.ResponseWriter,
	r *http.Request,
	evtType domain.EventType,
	parse func(map[string]json.RawMessage) (*eventParams, error),
) {
	rid := requestIDFromCtx(r.Context())
	creatorID := chi.URLParam(r, "id")
	viewerID := r.Header.Get("X-Viewer-Id")

	if viewerID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_VIEWER_ID", "X-Viewer-Id header is required", rid)
		return
	}

	if _, err := h.store.GetCreator(r.Context(), creatorID); err != nil {
		writeError(w, http.StatusNotFound, "CREATOR_NOT_FOUND", "creator not found", rid)
		return
	}

	bodyMap := map[string]json.RawMessage{}
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err == nil {
		bodyMap["_body"] = raw
	}

	params, err := parse(bodyMap)
	if err != nil {
		var ve *validationErr
		if errors.As(err, &ve) {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", ve.Error(), rid)
			return
		}
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is not valid JSON", rid)
		return
	}

	cid, _ := uuid.Parse(creatorID)
	evt := domain.Event{
		ID:          uuid.New(),
		CreatorID:   cid,
		ViewerID:    viewerID,
		EventType:   evtType,
		Tier:        params.tier,
		Quantity:    params.quantity,
		AmountCents: params.amountCents,
		CreatedAt:   time.Now().UTC(),
	}

	if err := h.pipeline.Submit(evt); err != nil {
		h.log.WarnContext(r.Context(), "pipeline full",
			slog.String("creator_id", creatorID),
			slog.String("event_type", string(evtType)),
		)
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "BACKPRESSURE", "server busy, retry shortly", rid)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"id": evt.ID.String()}, rid)
}

// GET /creators/{id}/revenue
func (h *Handlers) Revenue(w http.ResponseWriter, r *http.Request) {
	rid := requestIDFromCtx(r.Context())
	creatorID := chi.URLParam(r, "id")

	if _, err := h.store.GetCreator(r.Context(), creatorID); err != nil {
		writeError(w, http.StatusNotFound, "CREATOR_NOT_FOUND", "creator not found", rid)
		return
	}

	q := r.URL.Query()
	since := q.Get("since")
	until := q.Get("until")
	groupBy := q.Get("group_by")

	result, err := h.store.GetRevenue(r.Context(), creatorID, since, until, groupBy)
	if err != nil {
		h.log.ErrorContext(r.Context(), "get revenue", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch revenue", rid)
		return
	}

	writeJSON(w, http.StatusOK, result, rid)
}

// GET /creators/{id}/events
func (h *Handlers) Events(w http.ResponseWriter, r *http.Request) {
	rid := requestIDFromCtx(r.Context())
	creatorID := chi.URLParam(r, "id")

	if _, err := h.store.GetCreator(r.Context(), creatorID); err != nil {
		writeError(w, http.StatusNotFound, "CREATOR_NOT_FOUND", "creator not found", rid)
		return
	}

	q := r.URL.Query()
	evtType := q.Get("type")
	cursor := q.Get("cursor")
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	events, nextCursor, err := h.store.GetEvents(r.Context(), creatorID, evtType, cursor, limit)
	if err != nil {
		h.log.ErrorContext(r.Context(), "get events", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch events", rid)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events":      events,
		"next_cursor": nextCursor,
	}, rid)
}

// GET /leaderboard
func (h *Handlers) Leaderboard(w http.ResponseWriter, r *http.Request) {
	rid := requestIDFromCtx(r.Context())

	q := r.URL.Query()
	window := q.Get("window")
	if window == "" {
		window = "7d"
	}

	limit := 10
	entries := h.leaderboard.Top(limit)

	writeJSON(w, http.StatusOK, map[string]any{
		"window":  window,
		"entries": entries,
	}, rid)
}
