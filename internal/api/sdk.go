package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/eval"
	"github.com/jsarkisjanov/rulecast/internal/expr"
	"github.com/jsarkisjanov/rulecast/internal/store"
)

type evaluateRequest struct {
	FlagKey    string         `json:"flagKey"`
	Attributes map[string]any `json:"attributes"`
}

type evaluateAllRequest struct {
	Attributes map[string]any `json:"attributes"`
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}

	var request evaluateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if request.FlagKey == "" {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", "flagKey is required")
		return
	}

	schema, err := snapshot.Document().ExprSchema()
	if err != nil {
		writeError(w, s.logger, err)
		return
	}

	attributes := attributesFrom(request.Attributes, schema)
	result := snapshot.Evaluate(request.FlagKey, attributes)

	s.metrics.Evaluation(result.Reason)
	s.recordExposure(snapshot, result, attributes)

	writeJSON(w, s.logger, http.StatusOK, result)
}

func (s *Server) handleEvaluateAll(w http.ResponseWriter, r *http.Request) {
	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}

	var request evaluateAllRequest
	if err := decodeJSON(r, &request); err != nil {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	schema, err := snapshot.Document().ExprSchema()
	if err != nil {
		writeError(w, s.logger, err)
		return
	}

	attributes := attributesFrom(request.Attributes, schema)
	results := snapshot.EvaluateAll(attributes)

	for _, result := range results {
		s.metrics.Evaluation(result.Reason)
		s.recordExposure(snapshot, result, attributes)
	}

	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"version": snapshot.Version(),
		"results": results,
	})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}

	etag := fmt.Sprintf(`W/"%d"`, snapshot.Version())
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, s.logger, http.StatusOK, snapshot.Document())
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, s.logger, http.StatusInternalServerError, "NO_STREAMING",
			"the server cannot stream responses")
		return
	}

	updates, unsubscribe := s.registry.Subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	s.metrics.StreamOpened()
	defer s.metrics.StreamClosed()

	if snapshot := s.registry.Snapshot(); snapshot != nil {
		if !s.sendEvent(w, flusher, snapshot) {
			return
		}
	}

	heartbeat := time.NewTicker(s.config.Heartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case snapshot, open := <-updates:
			if !open {
				return
			}
			if !s.sendEvent(w, flusher, snapshot) {
				return
			}

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) sendEvent(w http.ResponseWriter, flusher http.Flusher, snapshot *eval.Snapshot) bool {
	payload, err := json.Marshal(snapshot.Document())
	if err != nil {
		s.logger.Error("encode snapshot for stream failed", "error", err)
		return false
	}

	if _, err := fmt.Fprintf(w, "id: %d\nevent: version\ndata: %s\n\n", snapshot.Version(), payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func (s *Server) recordExposure(snapshot *eval.Snapshot, result eval.Result, attributes expr.MapAttributes) {
	if s.exposures == nil {
		return
	}
	if result.Reason != eval.ReasonRollout && result.Reason != eval.ReasonRuleMatch {
		return
	}

	userKey := attributes.Lookup(eval.AttributeUserKey)
	if userKey.Kind() != expr.KindString || userKey.Str() == "" {
		return
	}

	s.exposures.Record(store.Exposure{
		Environment: snapshot.Environment(),
		FlagKey:     result.FlagKey,
		Variation:   result.Variation,
		Reason:      string(result.Reason),
		RuleID:      result.RuleID,
		UserKey:     userKey.Str(),
		Version:     result.Version,
		OccurredAt:  time.Now().UTC(),
	})
}
