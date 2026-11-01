package api

import (
	"fmt"
	"io"
	"sync/atomic"

	"github.com/jsarkisjanov/rulecast/internal/eval"
)

var trackedReasons = []eval.Reason{
	eval.ReasonRuleMatch,
	eval.ReasonRollout,
	eval.ReasonDefault,
	eval.ReasonFlagOff,
	eval.ReasonFlagNotFound,
	eval.ReasonMissingKey,
}

type Metrics struct {
	evaluations   atomic.Int64
	publishes     atomic.Int64
	streamClients atomic.Int64
	byReason      []atomic.Int64
	index         map[eval.Reason]int

	ExposuresDropped func() int64
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		byReason: make([]atomic.Int64, len(trackedReasons)),
		index:    make(map[eval.Reason]int, len(trackedReasons)),
	}
	for i, reason := range trackedReasons {
		metrics.index[reason] = i
	}
	return metrics
}

func (m *Metrics) Evaluation(reason eval.Reason) {
	m.evaluations.Add(1)
	if i, ok := m.index[reason]; ok {
		m.byReason[i].Add(1)
	}
}

func (m *Metrics) Publish()      { m.publishes.Add(1) }
func (m *Metrics) StreamOpened() { m.streamClients.Add(1) }
func (m *Metrics) StreamClosed() { m.streamClients.Add(-1) }

func (m *Metrics) WriteTo(w io.Writer, version int64, flags int) {
	if _, err := fmt.Fprint(w, "# HELP rulecast_evaluations_total Flag evaluations served.\n"); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "# TYPE rulecast_evaluations_total counter\n"); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "rulecast_evaluations_total %d\n", m.evaluations.Load()); err != nil {
		return
	}

	if _, err := fmt.Fprint(w, "# HELP rulecast_evaluations_by_reason_total Evaluations by outcome.\n"); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "# TYPE rulecast_evaluations_by_reason_total counter\n"); err != nil {
		return
	}
	for i, reason := range trackedReasons {
		if _, err := fmt.Fprintf(w,
			"rulecast_evaluations_by_reason_total{reason=%q} %d\n",
			string(reason), m.byReason[i].Load(),
		); err != nil {
			return
		}
	}

	if _, err := fmt.Fprint(w, "# HELP rulecast_publishes_total Ruleset versions published.\n"); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "# TYPE rulecast_publishes_total counter\n"); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "rulecast_publishes_total %d\n", m.publishes.Load()); err != nil {
		return
	}

	if _, err := fmt.Fprint(w, "# HELP rulecast_stream_clients Connected SSE clients.\n"); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "# TYPE rulecast_stream_clients gauge\n"); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "rulecast_stream_clients %d\n", m.streamClients.Load()); err != nil {
		return
	}

	if _, err := fmt.Fprint(w, "# HELP rulecast_active_version Version currently served.\n"); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "# TYPE rulecast_active_version gauge\n"); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "rulecast_active_version %d\n", version); err != nil {
		return
	}

	if _, err := fmt.Fprint(w, "# HELP rulecast_active_flags Flags in the active version.\n"); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "# TYPE rulecast_active_flags gauge\n"); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "rulecast_active_flags %d\n", flags); err != nil {
		return
	}

	if m.ExposuresDropped != nil {
		if _, err := fmt.Fprint(w, "# HELP rulecast_exposures_dropped_total Exposures dropped under backpressure.\n"); err != nil {
			return
		}
		if _, err := fmt.Fprint(w, "# TYPE rulecast_exposures_dropped_total counter\n"); err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "rulecast_exposures_dropped_total %d\n", m.ExposuresDropped()); err != nil {
			return
		}
	}
}
