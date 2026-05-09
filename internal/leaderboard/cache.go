package leaderboard

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Loader loads the leaderboard data from the DB.
type Loader interface {
	GetLeaderboard(ctx context.Context, window time.Duration) (map[string]int64, error)
}

type Entry struct {
	CreatorID string `json:"creator_id"`
	Cents     int64  `json:"amount_cents"`
}

type Cache struct {
	m         sync.Map // map[string]*atomic.Int64
	loader    Loader
	log       *slog.Logger
	refreshCh chan struct{}
	wg        sync.WaitGroup
}

func New(loader Loader, log *slog.Logger) *Cache {
	return &Cache{
		loader:    loader,
		log:       log,
		refreshCh: make(chan struct{}, 1),
	}
}

// Add increments the cached total for a creator. Safe for concurrent use.
func (c *Cache) Add(creatorID string, cents int64) {
	v, _ := c.m.LoadOrStore(creatorID, &atomic.Int64{})
	v.(*atomic.Int64).Add(cents)
}

// Top returns the top n creators by cached revenue, sorted descending.
func (c *Cache) Top(n int) []Entry {
	var entries []Entry
	c.m.Range(func(k, v any) bool {
		entries = append(entries, Entry{
			CreatorID: k.(string),
			Cents:     v.(*atomic.Int64).Load(),
		})
		return true
	})
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Cents > entries[j].Cents
	})
	if n > 0 && len(entries) > n {
		entries = entries[:n]
	}
	return entries
}

// Start launches the background refresh goroutine. It loads immediately, then every interval.
func (c *Cache) Start(ctx context.Context, interval time.Duration) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.reload(ctx, 7*24*time.Hour)

		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				c.reload(ctx, 7*24*time.Hour)
			case <-ctx.Done():
				return
			}
		}
	}()
	c.log.Info("leaderboard cache started", slog.Duration("refresh_interval", interval))
}

func (c *Cache) Shutdown() {
	c.wg.Wait()
}

func (c *Cache) reload(ctx context.Context, window time.Duration) {
	data, err := c.loader.GetLeaderboard(ctx, window)
	if err != nil {
		c.log.Warn("leaderboard reload failed", slog.String("error", err.Error()))
		return
	}
	for id, cents := range data {
		v, _ := c.m.LoadOrStore(id, &atomic.Int64{})
		v.(*atomic.Int64).Store(cents)
	}
	c.log.Debug("leaderboard reloaded", slog.Int("creators", len(data)))
}
