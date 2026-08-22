package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/expr"
	"github.com/jsarkisjanov/rulecast/internal/flag"
)

const defaultHistoryLimit = 50

type validateRequest struct {
	Expression string `json:"expression"`
}

type validateResponse struct {
	Valid      bool   `json:"valid"`
	Message    string `json:"message,omitempty"`
	Annotated  string `json:"annotated,omitempty"`
	Normalised string `json:"normalised,omitempty"`
}

type publishMeta struct {
	Comment   string `json:"comment,omitempty"`
	CreatedBy string `json:"createdBy,omitempty"`
}

type putFlagRequest struct {
	publishMeta
	Flag flag.Flag `json:"flag"`
}

type publishedResponse struct {
	Version int64  `json:"version"`
	Flags   int    `json:"flags"`
	Comment string `json:"comment,omitempty"`
}

func (s *Server) handleSchema(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, snapshot.Document().Schema)
}

func (s *Server) handleListFlags(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}

	document := snapshot.Document()
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"environment": document.Environment,
		"version":     document.Version,
		"flags":       document.Flags,
	})
}

func (s *Server) handleGetFlag(w http.ResponseWriter, r *http.Request) {
	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}

	found, ok := snapshot.Document().Flag(r.PathValue("key"))
	if !ok {
		writeProblem(w, s.logger, http.StatusNotFound, "NOT_FOUND", "no such flag")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, found)
}

func (s *Server) handlePutFlag(w http.ResponseWriter, r *http.Request) {
	var request putFlagRequest
	if err := decodeJSON(r, &request); err != nil {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	key := r.PathValue("key")
	if request.Flag.Key == "" {
		request.Flag.Key = key
	}
	if request.Flag.Key != key {
		writeProblem(w, s.logger, http.StatusBadRequest, "KEY_MISMATCH",
			"the flag key in the body must match the path")
		return
	}

	s.publishModified(w, r, request.publishMeta, func(document flag.Document) flag.Document {
		return document.WithFlag(request.Flag)
	})
}

func (s *Server) handleDeleteFlag(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}
	if _, ok := snapshot.Document().Flag(key); !ok {
		writeProblem(w, s.logger, http.StatusNotFound, "NOT_FOUND", "no such flag")
		return
	}

	meta := publishMeta{Comment: "deleted flag " + key, CreatedBy: r.Header.Get("X-Actor")}
	s.publishModified(w, r, meta, func(document flag.Document) flag.Document {
		return document.WithoutFlag(key)
	})
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	var request validateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	schema := expr.DefaultSchema()
	if snapshot := s.registry.Snapshot(); snapshot != nil {
		if current, err := snapshot.Document().ExprSchema(); err == nil {
			schema = current
		}
	}

	_, node, err := expr.Compile(request.Expression, schema)
	if err != nil {
		response := validateResponse{Valid: false, Message: err.Error()}
		var exprError *expr.Error
		if errors.As(err, &exprError) {
			response.Annotated = exprError.Annotated()
		}
		writeJSON(w, s.logger, http.StatusOK, response)
		return
	}

	writeJSON(w, s.logger, http.StatusOK, validateResponse{Valid: true, Normalised: node.String()})
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	limit := defaultHistoryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", "limit must be a positive integer")
			return
		}
		limit = min(parsed, 500)
	}

	history, err := s.versions.History(r.Context(), s.config.Environment, limit)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, history)
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	document, err := s.versions.Get(r.Context(), s.config.Environment, version)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, document)
}

func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	target, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	source, err := s.versions.Get(r.Context(), s.config.Environment, target)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}

	next, err := s.versions.NextVersion(r.Context(), s.config.Environment)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}

	document := source
	document.Version = next
	document.CreatedAt = time.Now().UTC()
	document.CreatedBy = r.Header.Get("X-Actor")
	document.Comment = "rollback to version " + strconv.FormatInt(target, 10)

	snapshot, err := s.publish(r.Context(), document)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}

	writeJSON(w, s.logger, http.StatusOK, publishedResponse{
		Version: snapshot.Version(),
		Flags:   len(snapshot.Keys()),
		Comment: document.Comment,
	})
}

func (s *Server) publishModified(
	w http.ResponseWriter,
	r *http.Request,
	meta publishMeta,
	modify func(flag.Document) flag.Document,
) {
	snapshot := s.registry.Snapshot()
	if snapshot == nil {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY", "no version loaded")
		return
	}

	next, err := s.versions.NextVersion(r.Context(), s.config.Environment)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}

	document := modify(snapshot.Document())
	document.Version = next
	document.CreatedAt = time.Now().UTC()
	document.Comment = meta.Comment
	document.CreatedBy = meta.CreatedBy
	if document.CreatedBy == "" {
		document.CreatedBy = r.Header.Get("X-Actor")
	}

	published, err := s.publish(r.Context(), document)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}

	writeJSON(w, s.logger, http.StatusOK, publishedResponse{
		Version: published.Version(),
		Flags:   len(published.Keys()),
		Comment: document.Comment,
	})
}

func (s *Server) handleExperiment(w http.ResponseWriter, r *http.Request) {
	if s.reports == nil {
		writeProblem(w, s.logger, http.StatusNotImplemented, "NO_REPORTS",
			"exposure reporting needs a database")
		return
	}

	from, to, err := timeWindow(r)
	if err != nil {
		writeProblem(w, s.logger, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	report, err := s.reports.Experiment(r.Context(), s.config.Environment, r.PathValue("key"), from, to)
	if err != nil {
		writeError(w, s.logger, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, report)
}

func timeWindow(r *http.Request) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30)

	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from must be RFC3339, got %q", raw)
		}
		from = parsed.UTC()
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to must be RFC3339, got %q", raw)
		}
		to = parsed.UTC()
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be before to")
	}
	return from, to, nil
}
