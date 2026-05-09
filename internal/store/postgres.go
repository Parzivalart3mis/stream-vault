package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yashk/streamvault/internal/domain"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("creating pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) CreateCreator(ctx context.Context, c *domain.Creator) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO creators (id, display_name, created_at) VALUES ($1, $2, $3)`,
		c.ID, c.DisplayName, c.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting creator: %w", err)
	}
	return nil
}

func (s *Store) GetCreator(ctx context.Context, id string) (*domain.Creator, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, display_name, created_at FROM creators WHERE id = $1`,
		id,
	)
	var c domain.Creator
	if err := row.Scan(&c.ID, &c.DisplayName, &c.CreatedAt); err != nil {
		return nil, fmt.Errorf("scanning creator: %w", err)
	}
	return &c, nil
}

func (s *Store) InsertEvent(ctx context.Context, e *domain.Event) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO events (id, creator_id, viewer_id, event_type, tier, quantity, amount_cents, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.CreatorID, e.ViewerID, e.EventType, e.Tier, e.Quantity, e.AmountCents, e.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting event: %w", err)
	}
	return nil
}

func (s *Store) InsertEventsBatch(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}

	rows := make([][]any, len(events))
	for i, e := range events {
		rows[i] = []any{e.ID, e.CreatorID, e.ViewerID, e.EventType, e.Tier, e.Quantity, e.AmountCents, e.CreatedAt}
	}

	_, err := s.pool.CopyFrom(ctx,
		[]string{"events"},
		[]string{"id", "creator_id", "viewer_id", "event_type", "tier", "quantity", "amount_cents", "created_at"},
		newRowSource(rows),
	)
	if err != nil {
		return fmt.Errorf("batch inserting events: %w", err)
	}
	return nil
}

type rowSource struct {
	rows [][]any
	pos  int
}

func newRowSource(rows [][]any) *rowSource {
	return &rowSource{rows: rows}
}

func (rs *rowSource) Next() bool {
	return rs.pos < len(rs.rows)
}

func (rs *rowSource) Values() ([]any, error) {
	row := rs.rows[rs.pos]
	rs.pos++
	return row, nil
}

func (rs *rowSource) Err() error { return nil }
