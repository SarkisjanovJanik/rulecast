package store

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ExposureWriterConfig struct {
	BufferSize    int
	BatchSize     int
	FlushInterval time.Duration
}

func (c ExposureWriterConfig) withDefaults() ExposureWriterConfig {
	if c.BufferSize <= 0 {
		c.BufferSize = 8192
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 500
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = time.Second
	}
	return c
}

type ExposureWriter struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	config ExposureWriterConfig
	queue  chan Exposure
	done   chan struct{}

	dropped atomic.Int64
}

func NewExposureWriter(pool *pgxpool.Pool, logger *slog.Logger, config ExposureWriterConfig) *ExposureWriter {
	config = config.withDefaults()
	return &ExposureWriter{
		pool:   pool,
		logger: logger,
		config: config,
		queue:  make(chan Exposure, config.BufferSize),
		done:   make(chan struct{}),
	}
}

func (w *ExposureWriter) Record(exposure Exposure) {
	select {
	case w.queue <- exposure:
	default:
		w.dropped.Add(1)
	}
}

func (w *ExposureWriter) Dropped() int64 { return w.dropped.Load() }

func (w *ExposureWriter) Run(ctx context.Context) {
	defer close(w.done)

	ticker := time.NewTicker(w.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]Exposure, 0, w.config.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.copy(context.WithoutCancel(ctx), batch); err != nil {
			w.logger.Error("exposure flush failed", "rows", len(batch), "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case exposure := <-w.queue:
					batch = append(batch, exposure)
					if len(batch) >= w.config.BatchSize {
						flush()
					}
					continue
				default:
				}
				break
			}
			flush()
			return

		case exposure := <-w.queue:
			batch = append(batch, exposure)
			if len(batch) >= w.config.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (w *ExposureWriter) Wait() { <-w.done }

func (w *ExposureWriter) copy(ctx context.Context, batch []Exposure) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows := make([][]any, len(batch))
	for i, exposure := range batch {
		rows[i] = []any{
			exposure.Environment, exposure.FlagKey, exposure.Variation, exposure.Reason,
			nullable(exposure.RuleID), exposure.UserKey, exposure.Version, exposure.OccurredAt,
		}
	}

	_, err := w.pool.CopyFrom(ctx,
		pgx.Identifier{"exposure"},
		[]string{"environment", "flag_key", "variation", "reason", "rule_id",
			"user_key", "version", "occurred_at"},
		pgx.CopyFromRows(rows))
	return err
}
