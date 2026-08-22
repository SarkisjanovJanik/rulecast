package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type contextKey string

const requestIDKey contextKey = "requestID"

const headerAPIKey = "X-API-Key"

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

type middleware func(http.Handler) http.Handler

func chain(handler http.Handler, wrappers ...middleware) http.Handler {
	for i := len(wrappers) - 1; i >= 0; i-- {
		handler = wrappers[i](handler)
	}
	return handler
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func withRecovery(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.Error("panic in handler",
					"path", r.URL.Path,
					"requestId", RequestID(r.Context()),
					"panic", recovered,
					"stack", string(debug.Stack()))
				writeProblem(w, logger, http.StatusInternalServerError, "INTERNAL", "unexpected server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func withLogging(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(recorder, r)

			if recorder.status == 0 {
				recorder.status = http.StatusOK
			}
			level := slog.LevelInfo
			if recorder.status >= 500 {
				level = slog.LevelError
			}

			logger.Log(r.Context(), level, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"bytes", recorder.bytes,
				"durationUs", time.Since(started).Microseconds(),
				"requestId", RequestID(r.Context()))
		})
	}
}

func requireKey(logger *slog.Logger, expected string) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expected == "" {
				next.ServeHTTP(w, r)
				return
			}

			presented := r.Header.Get(headerAPIKey)
			if subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
				writeProblem(w, logger, http.StatusUnauthorized, "UNAUTHORIZED", "a valid API key is required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func newRequestID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buffer)
}
