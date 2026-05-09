package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/yashk/streamvault/internal/domain"
)

type RevenueRow struct {
	EventType  string `json:"event_type"`
	TotalCents int64  `json:"total_cents"`
	EventCount int64  `json:"event_count"`
}

func (s *Store) GetRevenue(ctx context.Context, creatorID, since, until, groupBy string) (any, error) {
	args := []any{creatorID}
	where := []string{"creator_id = $1"}
	idx := 2

	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return nil, fmt.Errorf("invalid since: %w", err)
		}
		where = append(where, fmt.Sprintf("created_at >= $%d", idx))
		args = append(args, t.UTC())
		idx++
	}
	if until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return nil, fmt.Errorf("invalid until: %w", err)
		}
		where = append(where, fmt.Sprintf("created_at <= $%d", idx))
		args = append(args, t.UTC())
		idx++
	}

	_ = idx

	whereClause := strings.Join(where, " AND ")

	if groupBy == "type" {
		rows, err := s.pool.Query(ctx,
			fmt.Sprintf(`SELECT event_type, SUM(amount_cents) AS total_cents, COUNT(*) AS event_count
			             FROM events WHERE %s GROUP BY event_type ORDER BY total_cents DESC`, whereClause),
			args...,
		)
		if err != nil {
			return nil, fmt.Errorf("querying revenue by type: %w", err)
		}
		defer rows.Close()

		var result []RevenueRow
		for rows.Next() {
			var row RevenueRow
			if err := rows.Scan(&row.EventType, &row.TotalCents, &row.EventCount); err != nil {
				return nil, fmt.Errorf("scanning revenue row: %w", err)
			}
			result = append(result, row)
		}
		return result, rows.Err()
	}

	// Aggregate total
	var totalCents int64
	var eventCount int64
	err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COALESCE(SUM(amount_cents),0), COUNT(*) FROM events WHERE %s`, whereClause),
		args...,
	).Scan(&totalCents, &eventCount)
	if err != nil {
		return nil, fmt.Errorf("querying revenue: %w", err)
	}
	return map[string]any{"total_cents": totalCents, "event_count": eventCount}, nil
}

func (s *Store) GetEvents(ctx context.Context, creatorID, evtType, cursor string, limit int) ([]domain.Event, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args := []any{creatorID, limit + 1}
	where := []string{"creator_id = $1"}
	idx := 3

	if evtType != "" {
		where = append(where, fmt.Sprintf("event_type = $%d", idx))
		args = append(args, evtType)
		idx++
	}

	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		parts := strings.SplitN(string(decoded), ",", 2)
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("malformed cursor")
		}
		cursorTime, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, "", fmt.Errorf("cursor time parse: %w", err)
		}
		cursorID := parts[1]
		where = append(where, fmt.Sprintf("(created_at, id) < ($%d, $%d)", idx, idx+1))
		args = append(args, cursorTime.UTC(), cursorID)
		idx += 2
	}

	_ = idx

	whereClause := strings.Join(where, " AND ")

	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT id, creator_id, viewer_id, event_type, tier, quantity, amount_cents, created_at
		             FROM events WHERE %s ORDER BY created_at DESC, id DESC LIMIT $2`, whereClause),
		args...,
	)
	if err != nil {
		return nil, "", fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		if err := rows.Scan(&e.ID, &e.CreatorID, &e.ViewerID, &e.EventType, &e.Tier, &e.Quantity, &e.AmountCents, &e.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scanning event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		raw := last.CreatedAt.UTC().Format(time.RFC3339Nano) + "," + last.ID.String()
		nextCursor = base64.StdEncoding.EncodeToString([]byte(raw))
	}

	return events, nextCursor, nil
}

func (s *Store) GetLeaderboard(ctx context.Context, window time.Duration) (map[string]int64, error) {
	since := time.Now().UTC().Add(-window)
	rows, err := s.pool.Query(ctx,
		`SELECT creator_id, SUM(amount_cents) FROM events WHERE created_at >= $1 GROUP BY creator_id`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("querying leaderboard: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var id string
		var cents int64
		if err := rows.Scan(&id, &cents); err != nil {
			return nil, fmt.Errorf("scanning leaderboard row: %w", err)
		}
		result[id] = cents
	}
	return result, rows.Err()
}
