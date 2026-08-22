package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/flag"
)

type Memory struct {
	mu        sync.RWMutex
	documents map[string]map[int64]flag.Document
	exposures []Exposure
}

func NewMemory() *Memory {
	return &Memory{documents: make(map[string]map[int64]flag.Document)}
}

func (m *Memory) Latest(_ context.Context, environment string) (flag.Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	versions, ok := m.documents[environment]
	if !ok || len(versions) == 0 {
		return flag.Document{}, fmt.Errorf("environment %q: %w", environment, flag.ErrNotFound)
	}

	var latest int64
	for version := range versions {
		if version > latest {
			latest = version
		}
	}
	return versions[latest], nil
}

func (m *Memory) Get(_ context.Context, environment string, version int64) (flag.Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	document, ok := m.documents[environment][version]
	if !ok {
		return flag.Document{}, fmt.Errorf("environment %q version %d: %w",
			environment, version, flag.ErrNotFound)
	}
	return document, nil
}

func (m *Memory) History(_ context.Context, environment string, limit int) ([]VersionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]VersionInfo, 0, len(m.documents[environment]))
	for _, document := range m.documents[environment] {
		infos = append(infos, VersionInfo{
			Environment: document.Environment,
			Version:     document.Version,
			Comment:     document.Comment,
			CreatedBy:   document.CreatedBy,
			CreatedAt:   document.CreatedAt,
			Flags:       len(document.Flags),
		})
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Version > infos[j].Version })
	if limit > 0 && len(infos) > limit {
		infos = infos[:limit]
	}
	return infos, nil
}

func (m *Memory) Append(_ context.Context, document flag.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	versions, ok := m.documents[document.Environment]
	if !ok {
		versions = make(map[int64]flag.Document)
		m.documents[document.Environment] = versions
	}
	if _, exists := versions[document.Version]; exists {
		return fmt.Errorf("environment %q version %d: %w",
			document.Environment, document.Version, flag.ErrVersionExists)
	}

	versions[document.Version] = document
	return nil
}

func (m *Memory) NextVersion(_ context.Context, environment string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var latest int64
	for version := range m.documents[environment] {
		if version > latest {
			latest = version
		}
	}
	return latest + 1, nil
}

func (m *Memory) Record(exposure Exposure) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exposures = append(m.exposures, exposure)
}

func (m *Memory) RecordedExposures() []Exposure {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Exposure(nil), m.exposures...)
}

func (m *Memory) Experiment(
	_ context.Context,
	environment, flagKey string,
	from, to time.Time,
) (ExperimentReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := map[string]int64{}
	users := map[string]map[string]struct{}{}

	report := ExperimentReport{Environment: environment, FlagKey: flagKey, From: from, To: to}

	for _, exposure := range m.exposures {
		if exposure.Environment != environment || exposure.FlagKey != flagKey {
			continue
		}
		if exposure.OccurredAt.Before(from) || !exposure.OccurredAt.Before(to) {
			continue
		}
		counts[exposure.Variation]++
		if _, ok := users[exposure.Variation]; !ok {
			users[exposure.Variation] = map[string]struct{}{}
		}
		users[exposure.Variation][exposure.UserKey] = struct{}{}
		report.Exposures++
	}

	for variation, exposures := range counts {
		report.Variations = append(report.Variations, VariationStat{
			Variation: variation,
			Exposures: exposures,
			Users:     int64(len(users[variation])),
		})
		report.Users += int64(len(users[variation]))
	}

	sort.Slice(report.Variations, func(i, j int) bool {
		return report.Variations[i].Exposures > report.Variations[j].Exposures
	})

	withShares(&report)
	return report, nil
}
