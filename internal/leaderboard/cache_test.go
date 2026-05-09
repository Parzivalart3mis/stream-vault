package leaderboard

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubLoader struct {
	data map[string]int64
	err  error
}

func (s *stubLoader) GetLeaderboard(_ context.Context, _ time.Duration) (map[string]int64, error) {
	return s.data, s.err
}

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(&stubLoader{data: map[string]int64{}}, log)
}

func TestTop_Empty(t *testing.T) {
	c := newTestCache(t)
	assert.Empty(t, c.Top(10))
}

func TestTop_Sorted(t *testing.T) {
	c := newTestCache(t)
	c.Add("creator-b", 500)
	c.Add("creator-a", 1000)
	c.Add("creator-c", 250)

	entries := c.Top(10)
	require.Len(t, entries, 3)
	assert.Equal(t, "creator-a", entries[0].CreatorID)
	assert.Equal(t, int64(1000), entries[0].Cents)
	assert.Equal(t, "creator-b", entries[1].CreatorID)
	assert.Equal(t, "creator-c", entries[2].CreatorID)
}

func TestTop_Limit(t *testing.T) {
	c := newTestCache(t)
	for i := int64(1); i <= 20; i++ {
		c.Add("creator-"+string(rune('a'+i)), i*100)
	}
	assert.Len(t, c.Top(10), 10)
	assert.Len(t, c.Top(5), 5)
}

func TestAdd_Accumulates(t *testing.T) {
	c := newTestCache(t)
	c.Add("creator-x", 100)
	c.Add("creator-x", 200)
	c.Add("creator-x", 300)

	entries := c.Top(1)
	require.Len(t, entries, 1)
	assert.Equal(t, int64(600), entries[0].Cents)
}

func TestAdd_Concurrent(t *testing.T) {
	c := newTestCache(t)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Add("creator-race", 10)
		}()
	}
	wg.Wait()

	entries := c.Top(1)
	require.Len(t, entries, 1)
	assert.Equal(t, int64(1000), entries[0].Cents)
}

func TestReload_SetsValues(t *testing.T) {
	loader := &stubLoader{data: map[string]int64{
		"creator-1": 9999,
		"creator-2": 1234,
	}}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := New(loader, log)
	c.reload(context.Background(), 7*24*time.Hour)

	entries := c.Top(10)
	require.Len(t, entries, 2)
	assert.Equal(t, "creator-1", entries[0].CreatorID)
	assert.Equal(t, int64(9999), entries[0].Cents)
}
