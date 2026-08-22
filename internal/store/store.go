package store

import (
	"context"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/flag"
)

type VersionInfo struct {
	Environment string    `json:"environment"`
	Version     int64     `json:"version"`
	Comment     string    `json:"comment,omitempty"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	Flags       int       `json:"flags"`
}

type Versions interface {
	Latest(ctx context.Context, environment string) (flag.Document, error)
	Get(ctx context.Context, environment string, version int64) (flag.Document, error)
	History(ctx context.Context, environment string, limit int) ([]VersionInfo, error)
	Append(ctx context.Context, document flag.Document) error
	NextVersion(ctx context.Context, environment string) (int64, error)
}

type Exposure struct {
	Environment string    `json:"environment"`
	FlagKey     string    `json:"flagKey"`
	Variation   string    `json:"variation"`
	Reason      string    `json:"reason"`
	RuleID      string    `json:"ruleId,omitempty"`
	UserKey     string    `json:"userKey"`
	Version     int64     `json:"version"`
	OccurredAt  time.Time `json:"occurredAt"`
}

type Exposures interface {
	Record(exposure Exposure)
}

type VariationStat struct {
	Variation string  `json:"variation"`
	Exposures int64   `json:"exposures"`
	Users     int64   `json:"users"`
	Share     float64 `json:"share"`
}

type ExperimentReport struct {
	Environment string          `json:"environment"`
	FlagKey     string          `json:"flagKey"`
	From        time.Time       `json:"from"`
	To          time.Time       `json:"to"`
	Exposures   int64           `json:"exposures"`
	Users       int64           `json:"users"`
	Variations  []VariationStat `json:"variations"`
}

type Reports interface {
	Experiment(ctx context.Context, environment, flagKey string, from, to time.Time) (ExperimentReport, error)
}
