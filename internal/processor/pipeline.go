package processor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/yashk/streamvault/internal/domain"
)

// Store is the subset of store.Store the pipeline needs.
type Store interface {
	InsertEventsBatch(ctx context.Context, events []domain.Event) error
}

// CacheUpdater is the subset of leaderboard.Cache the pipeline needs.
type CacheUpdater interface {
	Add(creatorID string, cents int64)
}

const (
	batchSize    = 100
	batchTimeout = 50 * time.Millisecond
)

type Pipeline struct {
	ch      chan domain.Event
	workers int
	store   Store
	cache   CacheUpdater
	wg      sync.WaitGroup
	log     *slog.Logger
}

func New(store Store, cache CacheUpdater, workers, capacity int, log *slog.Logger) *Pipeline {
	return &Pipeline{
		ch:      make(chan domain.Event, capacity),
		workers: workers,
		store:   store,
		cache:   cache,
		log:     log,
	}
}

func (p *Pipeline) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
	p.log.Info("pipeline started", slog.Int("workers", p.workers), slog.Int("capacity", cap(p.ch)))
}

// Submit enqueues an event. Returns domain.ErrChannelFull if the buffer is full.
func (p *Pipeline) Submit(e domain.Event) error {
	select {
	case p.ch <- e:
		return nil
	default:
		return domain.ErrChannelFull
	}
}

// Shutdown closes the channel and waits for workers to drain, respecting the deadline in ctx.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	close(p.ch)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.log.Info("pipeline drained")
		return nil
	case <-ctx.Done():
		p.log.Warn("pipeline drain deadline exceeded")
		return ctx.Err()
	}
}

func (p *Pipeline) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	batch := make([]domain.Event, 0, batchSize)
	timer := time.NewTimer(batchTimeout)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := p.store.InsertEventsBatch(ctx, batch); err != nil {
			p.log.Error("batch insert failed",
				slog.Int("worker", id),
				slog.Int("batch_size", len(batch)),
				slog.String("error", err.Error()),
			)
		} else {
			for _, e := range batch {
				p.cache.Add(e.CreatorID.String(), e.AmountCents)
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case e, ok := <-p.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, e)
			if len(batch) >= batchSize {
				flush()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(batchTimeout)
			}

		case <-timer.C:
			flush()
			timer.Reset(batchTimeout)

		case <-ctx.Done():
			flush()
			return
		}
	}
}
