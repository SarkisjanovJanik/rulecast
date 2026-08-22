package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/eval"
	"github.com/jsarkisjanov/rulecast/internal/expr"
	"github.com/jsarkisjanov/rulecast/internal/flag"
	"github.com/jsarkisjanov/rulecast/internal/store"
)

const (
	testSDKKey   = "sdk-key"
	testAdminKey = "admin-key"
)

type harness struct {
	server   *httptest.Server
	memory   *store.Memory
	registry *eval.Registry
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	memory := store.NewMemory()
	registry := eval.NewRegistry()

	document := flag.Seed("production", time.Unix(1_700_000_000, 0))
	if err := memory.Append(context.Background(), document); err != nil {
		t.Fatalf("seed: %v", err)
	}
	snapshot, err := eval.Compile(document)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	registry.Publish(snapshot)

	server := NewServer(Config{
		Environment: "production",
		SDKKey:      testSDKKey,
		AdminKey:    testAdminKey,
		Heartbeat:   50 * time.Millisecond,
	}, registry, memory, memory, memory, NewMetrics(), logger)

	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	return &harness{server: httpServer, memory: memory, registry: registry}
}

func (h *harness) do(t *testing.T, method, path, key string, body any) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set(headerAPIKey, key)
	}

	response, err := h.server.Client().Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func decodeBody[T any](t *testing.T, response *http.Response) T {
	t.Helper()
	var target T
	if err := json.NewDecoder(response.Body).Decode(&target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return target
}

func TestHealthAndReadinessAreUnauthenticated(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		response := h.do(t, http.MethodGet, path, "", nil)
		if response.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, response.StatusCode)
		}
	}
}

func TestSDKRequiresItsKey(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{"flagKey": "checkout-v2", "attributes": map[string]any{}}

	if response := h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", "", body); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("without a key = %d, want 401", response.StatusCode)
	}
	if response := h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", testAdminKey, body); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("with the admin key = %d, want 401", response.StatusCode)
	}
	if response := h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", testSDKKey, body); response.StatusCode != http.StatusOK {
		t.Errorf("with the sdk key = %d, want 200", response.StatusCode)
	}
}

func TestEvaluateAppliesRules(t *testing.T) {
	h := newHarness(t)

	response := h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", testSDKKey, map[string]any{
		"flagKey": "checkout-v2",
		"attributes": map[string]any{
			"user.key":     "u-1",
			"user.country": "DE",
			"user.tier":    "vip",
			"user.spend":   400,
			"user.beta":    false,
		},
	})

	result := decodeBody[eval.Result](t, response)
	if result.Reason != eval.ReasonRuleMatch {
		t.Errorf("reason = %s, want %s", result.Reason, eval.ReasonRuleMatch)
	}
	if result.RuleID != "vip-dach" {
		t.Errorf("ruleId = %q, want \"vip-dach\"", result.RuleID)
	}
	if string(result.Value) != "true" {
		t.Errorf("value = %s, want true", result.Value)
	}
}

func TestEvaluateIgnoresAttributesOutsideTheSchema(t *testing.T) {
	h := newHarness(t)

	response := h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", testSDKKey, map[string]any{
		"flagKey": "checkout-v2",
		"attributes": map[string]any{
			"user.key":       "u-1",
			"user.beta":      true,
			"unknown.field":  "ignored",
			"another.number": 42,
		},
	})

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; unknown attributes must not fail the request", response.StatusCode)
	}
	if result := decodeBody[eval.Result](t, response); result.Variation != "on" {
		t.Errorf("variation = %q, want \"on\"", result.Variation)
	}
}

func TestEvaluateRecordsExposuresOnlyForAssignments(t *testing.T) {
	h := newHarness(t)

	h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", testSDKKey, map[string]any{
		"flagKey":    "checkout-v2",
		"attributes": map[string]any{"user.key": "u-1", "user.beta": true},
	})
	h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", testSDKKey, map[string]any{
		"flagKey":    "maintenance-mode",
		"attributes": map[string]any{"user.key": "u-1"},
	})

	exposures := h.memory.RecordedExposures()
	if len(exposures) != 1 {
		t.Fatalf("recorded %d exposures, want 1 (the disabled flag must not count)", len(exposures))
	}
	if exposures[0].FlagKey != "checkout-v2" || exposures[0].UserKey != "u-1" {
		t.Errorf("unexpected exposure %+v", exposures[0])
	}
}

func TestSnapshotUsesTheVersionAsETag(t *testing.T) {
	h := newHarness(t)

	first := h.do(t, http.MethodGet, "/api/v1/sdk/snapshot", testSDKKey, nil)
	etag := first.Header.Get("ETag")
	if etag == "" {
		t.Fatal("expected an ETag")
	}

	request, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/sdk/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(headerAPIKey, testSDKKey)
	request.Header.Set("If-None-Match", etag)

	second, err := h.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()

	if second.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304", second.StatusCode)
	}
}

func TestValidateReportsProblemsWithoutFailing(t *testing.T) {
	h := newHarness(t)

	response := h.do(t, http.MethodPost, "/api/v1/admin/validate", testAdminKey, map[string]any{
		"expression": `user.contry == "DE"`,
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; a bad rule is an answer, not an error", response.StatusCode)
	}

	body := decodeBody[validateResponse](t, response)
	if body.Valid {
		t.Error("expected the expression to be rejected")
	}
	if !strings.Contains(body.Message, "unknown attribute") {
		t.Errorf("message = %q, want it to mention the unknown attribute", body.Message)
	}
	if !strings.Contains(body.Annotated, "^") {
		t.Errorf("annotated output should carry a caret, got %q", body.Annotated)
	}
}

func TestPutFlagPublishesANewVersion(t *testing.T) {
	h := newHarness(t)

	before := h.registry.Snapshot().Version()

	updated := flag.Flag{
		Key:          "maintenance-mode",
		Kind:         flag.KindBool,
		Enabled:      true,
		Salt:         "maintenance-2026",
		Variations:   []flag.Variation{{Key: "on", Value: json.RawMessage("true")}, {Key: "off", Value: json.RawMessage("false")}},
		Default:      "on",
		OffVariation: "off",
	}

	response := h.do(t, http.MethodPut, "/api/v1/admin/flags/maintenance-mode", testAdminKey, map[string]any{
		"comment":   "enable maintenance",
		"createdBy": "ada",
		"flag":      updated,
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	published := decodeBody[publishedResponse](t, response)
	if published.Version != before+1 {
		t.Errorf("version = %d, want %d", published.Version, before+1)
	}
	if got := h.registry.Snapshot().Version(); got != published.Version {
		t.Errorf("registry serves version %d, want %d", got, published.Version)
	}

	result := h.registry.Snapshot().Evaluate("maintenance-mode", expr.MapAttributes{})
	if result.Variation != "on" {
		t.Errorf("variation = %q, want \"on\" after the change", result.Variation)
	}
}

func TestPutFlagRejectsABrokenRuleAndKeepsTheOldVersion(t *testing.T) {
	h := newHarness(t)
	before := h.registry.Snapshot().Version()

	broken := flag.Flag{
		Key:          "checkout-v2",
		Kind:         flag.KindBool,
		Enabled:      true,
		Salt:         "checkout-v2-2026",
		Variations:   []flag.Variation{{Key: "on", Value: json.RawMessage("true")}, {Key: "off", Value: json.RawMessage("false")}},
		Default:      "off",
		OffVariation: "off",
		Rules: []flag.Rule{
			{ID: "typo", Expression: `user.contry == "DE"`, Serve: flag.Serve{Variation: "on"}},
		},
	}

	response := h.do(t, http.MethodPut, "/api/v1/admin/flags/checkout-v2", testAdminKey, map[string]any{
		"flag": broken,
	})
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}

	problem := decodeBody[Problem](t, response)
	if len(problem.Problems) == 0 {
		t.Error("expected the response to list the problems")
	}
	if got := h.registry.Snapshot().Version(); got != before {
		t.Errorf("version moved to %d; a rejected change must not publish", got)
	}
}

func TestPutFlagRejectsAKeyMismatch(t *testing.T) {
	h := newHarness(t)

	response := h.do(t, http.MethodPut, "/api/v1/admin/flags/checkout-v2", testAdminKey, map[string]any{
		"flag": flag.Flag{Key: "something-else", Kind: flag.KindBool},
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", response.StatusCode)
	}
}

func TestRollbackRepublishesAnOlderVersion(t *testing.T) {
	h := newHarness(t)
	original := h.registry.Snapshot().Version()

	h.do(t, http.MethodDelete, "/api/v1/admin/flags/legacy-ios-warning", testAdminKey, nil)
	if _, ok := h.registry.Snapshot().Document().Flag("legacy-ios-warning"); ok {
		t.Fatal("the flag should be gone after the delete")
	}

	response := h.do(t, http.MethodPost,
		"/api/v1/admin/versions/"+strconv.FormatInt(original, 10)+"/rollback", testAdminKey, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	published := decodeBody[publishedResponse](t, response)
	if published.Version != original+2 {
		t.Errorf("version = %d, want %d; history must stay append-only", published.Version, original+2)
	}
	if _, ok := h.registry.Snapshot().Document().Flag("legacy-ios-warning"); !ok {
		t.Error("the rollback should have restored the flag")
	}
}

func TestVersionHistoryIsNewestFirst(t *testing.T) {
	h := newHarness(t)

	h.do(t, http.MethodDelete, "/api/v1/admin/flags/legacy-ios-warning", testAdminKey, nil)

	response := h.do(t, http.MethodGet, "/api/v1/admin/versions", testAdminKey, nil)
	history := decodeBody[[]store.VersionInfo](t, response)

	if len(history) != 2 {
		t.Fatalf("got %d versions, want 2", len(history))
	}
	if history[0].Version <= history[1].Version {
		t.Errorf("history is not newest first: %d then %d", history[0].Version, history[1].Version)
	}
}

func TestStreamSendsTheCurrentVersionThenUpdates(t *testing.T) {
	h := newHarness(t)

	request, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/sdk/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(headerAPIKey, testSDKKey)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	response, err := h.server.Client().Do(request.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	reader := bufio.NewReader(response.Body)
	if id := readEventID(t, reader); id != "1" {
		t.Errorf("first event id = %q, want \"1\"", id)
	}

	next := flag.Seed("production", time.Unix(1_700_000_000, 0))
	next.Version = 2
	snapshot, err := eval.Compile(next)
	if err != nil {
		t.Fatal(err)
	}
	h.registry.Publish(snapshot)

	if id := readEventID(t, reader); id != "2" {
		t.Errorf("second event id = %q, want \"2\"", id)
	}
}

func readEventID(t *testing.T, reader *bufio.Reader) string {
	t.Helper()

	deadline := time.After(3 * time.Second)
	lines := make(chan string, 1)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				close(lines)
				return
			}
			if strings.HasPrefix(line, "id: ") {
				lines <- strings.TrimSpace(strings.TrimPrefix(line, "id: "))
				return
			}
		}
	}()

	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatal("stream closed before an event arrived")
		}
		return line
	case <-deadline:
		t.Fatal("timed out waiting for a stream event")
		return ""
	}
}

func TestUnknownAdminRouteIs404(t *testing.T) {
	h := newHarness(t)

	if response := h.do(t, http.MethodGet, "/api/v1/admin/nope", testAdminKey, nil); response.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", response.StatusCode)
	}
}

func TestRejectsUnknownJSONFields(t *testing.T) {
	h := newHarness(t)

	response := h.do(t, http.MethodPost, "/api/v1/sdk/evaluate", testSDKKey, map[string]any{
		"flagKey":   "checkout-v2",
		"typoField": true,
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", response.StatusCode)
	}
}
