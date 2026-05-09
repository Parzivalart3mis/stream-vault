package processor

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yashk/streamvault/internal/domain"
)

type stubStore struct {
	count atomic.Int64
}

func (s *stubStore) InsertEventsBatch(_ context.Context, events []domain.Event) error {
	s.count.Add(int64(len(events)))
	return nil
}

type stubCache struct {
	count atomic.Int64
}

func (c *stubCache) Add(_ string, _ int64) {
	c.count.Add(1)
}

func newEvent() domain.Event {
	tier := 1
	return domain.Event{
		ID:          uuid.New(),
		CreatorID:   uuid.New(),
		ViewerID:    "viewer-1",
		EventType:   domain.EventSubscription,
		Tier:        &tier,
		Quantity:    1,
		AmountCents: 499,
		CreatedAt:   time.Now().UTC(),
	}
}

func TestPipeline_SubmitAndDrain(t *testing.T) {
	store := &stubStore{}
	cache := &stubCache{}
	log := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))

	p := New(store, cache, 2, 100, log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	const n = 50
	for i := 0; i < n; i++ {
		require.NoError(t, p.Submit(newEvent()))
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	require.NoError(t, p.Shutdown(shutCtx))

	assert.Equal(t, int64(n), store.count.Load())
	assert.Equal(t, int64(n), cache.count.Load())
}

func TestPipeline_ChannelFull(t *testing.T) {
	store := &stubStore{}
	cache := &stubCache{}
	log := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))

	// capacity 1 so we can fill it without starting workers
	p := New(store, cache, 0, 1, log)

	require.NoError(t, p.Submit(newEvent()))
	err := p.Submit(newEvent())
	assert.ErrorIs(t, err, domain.ErrChannelFull)
}

func TestPipeline_BatchFlushOnTimer(t *testing.T) {
	store := &stubStore{}
	cache := &stubCache{}
	log := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))

	p := New(store, cache, 1, 100, log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	// Send fewer events than batchSize so flush must happen via timer
	require.NoError(t, p.Submit(newEvent()))
	require.NoError(t, p.Submit(newEvent()))

	// Wait beyond the 50ms batch timer
	time.Sleep(150 * time.Millisecond)

	assert.Equal(t, int64(2), store.count.Load())
}
