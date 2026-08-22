package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jsarkisjanov/rulecast/internal/expr"
	"github.com/jsarkisjanov/rulecast/internal/flag"
)

const maxRequestBytes = 1 << 20

type Problem struct {
	Status   int            `json:"status"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Problems []flag.Problem `json:"problems,omitempty"`
}

func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Warn("write response failed", "error", err)
	}
}

func writeProblem(w http.ResponseWriter, logger *slog.Logger, status int, code, message string) {
	writeJSON(w, logger, status, Problem{Status: status, Code: code, Message: message})
}

func writeError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var validation *flag.ValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, logger, http.StatusUnprocessableEntity, Problem{
			Status:   http.StatusUnprocessableEntity,
			Code:     "INVALID_DOCUMENT",
			Message:  "the flag set is not valid",
			Problems: validation.Problems,
		})

	case errors.Is(err, flag.ErrNotFound):
		writeProblem(w, logger, http.StatusNotFound, "NOT_FOUND", err.Error())

	case errors.Is(err, flag.ErrVersionExists):
		writeProblem(w, logger, http.StatusConflict, "VERSION_CONFLICT",
			"another publish won the race; reload and try again")

	default:
		logger.Error("unhandled request failure", "error", err)
		writeProblem(w, logger, http.StatusInternalServerError, "INTERNAL", "unexpected server error")
	}
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}

func attributesFrom(raw map[string]any, schema expr.Schema) expr.MapAttributes {
	attributes := make(expr.MapAttributes, len(raw))

	for key, value := range raw {
		kind, ok := schema.Kind(key)
		if !ok {
			continue
		}

		switch kind {
		case expr.KindString:
			if typed, ok := value.(string); ok {
				attributes[key] = expr.String(typed)
			}
		case expr.KindNumber:
			if typed, ok := value.(float64); ok {
				attributes[key] = expr.Number(typed)
			}
		case expr.KindBool:
			if typed, ok := value.(bool); ok {
				attributes[key] = expr.Bool(typed)
			}
		}
	}

	return attributes
}
