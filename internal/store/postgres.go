package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jsarkisjanov/rulecast/internal/flag"
)

const uniqueViolation = "23505"

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) Latest(ctx context.Context, environment string) (flag.Document, error) {
	const query = `
		SELECT document
		FROM flag_set_version
		WHERE environment = $1
		ORDER BY version DESC
		LIMIT 1`

	var raw []byte
	if err := p.pool.QueryRow(ctx, query, environment).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flag.Document{}, fmt.Errorf("environment %q: %w", environment, flag.ErrNotFound)
		}
		return flag.Document{}, fmt.Errorf("select latest version: %w", err)
	}
	return decode(raw)
}

func (p *Postgres) Get(ctx context.Context, environment string, version int64) (flag.Document, error) {
	const query = `SELECT document FROM flag_set_version WHERE environment = $1 AND version = $2`

	var raw []byte
	if err := p.pool.QueryRow(ctx, query, environment, version).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flag.Document{}, fmt.Errorf("environment %q version %d: %w",
				environment, version, flag.ErrNotFound)
		}
		return flag.Document{}, fmt.Errorf("select version: %w", err)
	}
	return decode(raw)
}

func (p *Postgres) History(ctx context.Context, environment string, limit int) ([]VersionInfo, error) {
	const query = `
		SELECT environment, version, coalesce(comment, ''), coalesce(created_by, ''), created_at,
		       jsonb_array_length(document -> 'flags')
		FROM flag_set_version
		WHERE environment = $1
		ORDER BY version DESC
		LIMIT $2`

	rows, err := p.pool.Query(ctx, query, environment, limit)
	if err != nil {
		return nil, fmt.Errorf("select history: %w", err)
	}
	defer rows.Close()

	var infos []VersionInfo
	for rows.Next() {
		var info VersionInfo
		if err := rows.Scan(&info.Environment, &info.Version, &info.Comment,
			&info.CreatedBy, &info.CreatedAt, &info.Flags); err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}
		infos = append(infos, info)
	}
	return infos, rows.Err()
}

func (p *Postgres) Append(ctx context.Context, document flag.Document) error {
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode document: %w", err)
	}

	const insert = `
		INSERT INTO flag_set_version (environment, version, document, comment, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err = p.pool.Exec(ctx, insert, document.Environment, document.Version, encoded,
		nullable(document.Comment), nullable(document.CreatedBy), document.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return fmt.Errorf("environment %q version %d: %w",
				document.Environment, document.Version, flag.ErrVersionExists)
		}
		return fmt.Errorf("insert version: %w", err)
	}
	return nil
}

func (p *Postgres) NextVersion(ctx context.Context, environment string) (int64, error) {
	const query = `SELECT coalesce(max(version), 0) + 1 FROM flag_set_version WHERE environment = $1`

	var next int64
	if err := p.pool.QueryRow(ctx, query, environment).Scan(&next); err != nil {
		return 0, fmt.Errorf("select next version: %w", err)
	}
	return next, nil
}

func decode(raw []byte) (flag.Document, error) {
	var document flag.Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return flag.Document{}, fmt.Errorf("decode document: %w", err)
	}
	return document, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (p *Postgres) Experiment(
	ctx context.Context,
	environment, flagKey string,
	from, to time.Time,
) (ExperimentReport, error) {
	const query = `
		SELECT variation, count(*), count(DISTINCT user_key)
		FROM exposure
		WHERE environment = $1 AND flag_key = $2 AND occurred_at >= $3 AND occurred_at < $4
		GROUP BY variation
		ORDER BY 2 DESC`

	rows, err := p.pool.Query(ctx, query, environment, flagKey, from, to)
	if err != nil {
		return ExperimentReport{}, fmt.Errorf("select experiment report: %w", err)
	}
	defer rows.Close()

	report := ExperimentReport{Environment: environment, FlagKey: flagKey, From: from, To: to}
	for rows.Next() {
		var stat VariationStat
		if err := rows.Scan(&stat.Variation, &stat.Exposures, &stat.Users); err != nil {
			return ExperimentReport{}, fmt.Errorf("scan experiment row: %w", err)
		}
		report.Variations = append(report.Variations, stat)
		report.Exposures += stat.Exposures
		report.Users += stat.Users
	}
	if err := rows.Err(); err != nil {
		return ExperimentReport{}, fmt.Errorf("read experiment rows: %w", err)
	}

	withShares(&report)
	return report, nil
}

func withShares(report *ExperimentReport) {
	if report.Exposures == 0 {
		return
	}
	for i := range report.Variations {
		report.Variations[i].Share =
			float64(report.Variations[i].Exposures) / float64(report.Exposures)
	}
}
