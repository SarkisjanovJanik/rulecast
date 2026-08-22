package eval

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/expr"
	"github.com/jsarkisjanov/rulecast/internal/flag"
)

func raw(v any) json.RawMessage {
	encoded, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return encoded
}

func document() flag.Document {
	return flag.Document{
		Environment: "production",
		Version:     7,
		CreatedAt:   time.Unix(0, 0).UTC(),
		Schema: map[string]string{
			"user.key":     "string",
			"user.country": "string",
			"user.tier":    "string",
			"user.spend":   "number",
			"user.beta":    "bool",
		},
		Flags: []flag.Flag{
			{
				Key:     "checkout-v2",
				Kind:    flag.KindBool,
				Enabled: true,
				Salt:    "salt-checkout",
				Variations: []flag.Variation{
					{Key: "on", Value: raw(true)},
					{Key: "off", Value: raw(false)},
				},
				Default:      "off",
				OffVariation: "off",
				Rules: []flag.Rule{
					{
						ID:         "vip-de",
						Expression: `user.country == "DE" and user.tier == "vip"`,
						Serve:      flag.Serve{Variation: "on"},
					},
					{
						ID:         "gradual",
						Expression: `user.country == "PL"`,
						Serve: flag.Serve{Rollout: []flag.RolloutEntry{
							{Variation: "on", Weight: 25},
							{Variation: "off", Weight: 75},
						}},
					},
				},
			},
			{
				Key:     "banner-copy",
				Kind:    flag.KindString,
				Enabled: false,
				Salt:    "salt-banner",
				Variations: []flag.Variation{
					{Key: "control", Value: raw("Welcome")},
					{Key: "bold", Value: raw("WELCOME!")},
				},
				Default:      "bold",
				OffVariation: "control",
			},
		},
	}
}

func snapshot(t *testing.T) *Snapshot {
	t.Helper()
	compiled, err := Compile(document())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return compiled
}

func TestEvaluate(t *testing.T) {
	compiled := snapshot(t)

	tests := []struct {
		name       string
		flag       string
		attributes expr.MapAttributes
		variation  string
		reason     Reason
		ruleID     string
	}{
		{
			name: "rule match",
			flag: "checkout-v2",
			attributes: expr.MapAttributes{
				"user.key":     expr.String("u1"),
				"user.country": expr.String("DE"),
				"user.tier":    expr.String("vip"),
			},
			variation: "on", reason: ReasonRuleMatch, ruleID: "vip-de",
		},
		{
			name: "falls through to default",
			flag: "checkout-v2",
			attributes: expr.MapAttributes{
				"user.key":     expr.String("u2"),
				"user.country": expr.String("FR"),
			},
			variation: "off", reason: ReasonDefault,
		},
		{
			name: "disabled flag serves the off variation",
			flag: "banner-copy",
			attributes: expr.MapAttributes{
				"user.key": expr.String("u3"),
			},
			variation: "control", reason: ReasonFlagOff,
		},
		{
			name:       "unknown flag",
			flag:       "nope",
			attributes: expr.MapAttributes{"user.key": expr.String("u4")},
			reason:     ReasonFlagNotFound,
		},
		{
			name: "rollout without a user key cannot bucket",
			flag: "checkout-v2",
			attributes: expr.MapAttributes{
				"user.country": expr.String("PL"),
			},
			reason: ReasonMissingKey, ruleID: "gradual",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := compiled.Evaluate(test.flag, test.attributes)

			if result.Reason != test.reason {
				t.Errorf("reason = %s, want %s", result.Reason, test.reason)
			}
			if result.Variation != test.variation {
				t.Errorf("variation = %q, want %q", result.Variation, test.variation)
			}
			if result.RuleID != test.ruleID {
				t.Errorf("ruleId = %q, want %q", result.RuleID, test.ruleID)
			}
			if result.Version != 7 {
				t.Errorf("version = %d, want 7", result.Version)
			}
		})
	}
}

func TestEvaluateRolloutIsStableAndProportional(t *testing.T) {
	compiled := snapshot(t)

	const users = 20_000
	on := 0

	for i := range users {
		attributes := expr.MapAttributes{
			"user.key":     expr.String(fmt.Sprintf("user-%d", i)),
			"user.country": expr.String("PL"),
		}

		result := compiled.Evaluate("checkout-v2", attributes)
		if result.Reason != ReasonRollout {
			t.Fatalf("reason = %s, want %s", result.Reason, ReasonRollout)
		}
		if result.RuleID != "gradual" {
			t.Fatalf("ruleId = %q, want \"gradual\"", result.RuleID)
		}
		if again := compiled.Evaluate("checkout-v2", attributes); again.Variation != result.Variation {
			t.Fatalf("evaluation is not stable for %s", attributes["user.key"].Str())
		}
		if result.Variation == "on" {
			on++
		}
	}

	share := float64(on) / users * 100
	if share < 23.5 || share > 26.5 {
		t.Errorf("rollout served \"on\" to %.2f%% of users, expected about 25%%", share)
	}
}

func TestEvaluateFirstMatchingRuleWins(t *testing.T) {
	compiled := snapshot(t)

	result := compiled.Evaluate("checkout-v2", expr.MapAttributes{
		"user.key":     expr.String("u9"),
		"user.country": expr.String("DE"),
		"user.tier":    expr.String("vip"),
		"user.spend":   expr.Number(1),
	})

	if result.RuleID != "vip-de" {
		t.Errorf("ruleId = %q, want the first matching rule", result.RuleID)
	}
}

func TestEvaluateAllCoversEveryFlagInOrder(t *testing.T) {
	compiled := snapshot(t)

	results := compiled.EvaluateAll(expr.MapAttributes{"user.key": expr.String("u1")})
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].FlagKey != "banner-copy" || results[1].FlagKey != "checkout-v2" {
		t.Errorf("results are not sorted by key: %s, %s", results[0].FlagKey, results[1].FlagKey)
	}
}

func TestCompileRejectsABrokenDocument(t *testing.T) {
	broken := document()
	broken.Flags[0].Rules[0].Expression = `user.contry == "DE"`

	if _, err := Compile(broken); err == nil {
		t.Fatal("expected Compile to reject an unknown attribute")
	}
}

func TestRegistrySwapsAtomically(t *testing.T) {
	registry := NewRegistry()
	first := snapshot(t)
	registry.Publish(first)

	if got := registry.Snapshot(); got.Version() != 7 {
		t.Fatalf("version = %d, want 7", got.Version())
	}

	next := document()
	next.Version = 8
	compiled, err := Compile(next)
	if err != nil {
		t.Fatal(err)
	}
	registry.Publish(compiled)

	if got := registry.Snapshot(); got.Version() != 8 {
		t.Errorf("version = %d, want 8", got.Version())
	}
}

func TestRegistryNotifiesSubscribers(t *testing.T) {
	registry := NewRegistry()
	updates, unsubscribe := registry.Subscribe()
	defer unsubscribe()

	registry.Publish(snapshot(t))

	select {
	case received := <-updates:
		if received.Version() != 7 {
			t.Errorf("version = %d, want 7", received.Version())
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber was not notified")
	}
}

func TestRegistryDropsSlowSubscribersInsteadOfBlocking(t *testing.T) {
	registry := NewRegistry()
	_, unsubscribe := registry.Subscribe()
	defer unsubscribe()

	compiled := snapshot(t)
	done := make(chan struct{})
	go func() {
		for range 100 {
			registry.Publish(compiled)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a subscriber that never reads")
	}
}

func TestRegistryUnsubscribeIsIdempotent(t *testing.T) {
	registry := NewRegistry()
	_, unsubscribe := registry.Subscribe()

	unsubscribe()
	unsubscribe()

	if got := registry.Subscribers(); got != 0 {
		t.Errorf("subscribers = %d, want 0", got)
	}
}

func TestRegistryIsSafeUnderConcurrentUse(t *testing.T) {
	registry := NewRegistry()
	registry.Publish(snapshot(t))

	var group sync.WaitGroup
	stop := make(chan struct{})

	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			attributes := expr.MapAttributes{"user.key": expr.String("u1")}
			for {
				select {
				case <-stop:
					return
				default:
					registry.Snapshot().Evaluate("checkout-v2", attributes)
				}
			}
		}()
	}

	for version := int64(8); version < 60; version++ {
		next := document()
		next.Version = version
		compiled, err := Compile(next)
		if err != nil {
			t.Fatal(err)
		}
		registry.Publish(compiled)
	}

	close(stop)
	group.Wait()
}

func BenchmarkEvaluateRuleMatch(b *testing.B) {
	compiled, err := Compile(document())
	if err != nil {
		b.Fatal(err)
	}
	attributes := expr.MapAttributes{
		"user.key":     expr.String("user-1234"),
		"user.country": expr.String("DE"),
		"user.tier":    expr.String("vip"),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		compiled.Evaluate("checkout-v2", attributes)
	}
}

func BenchmarkEvaluateRollout(b *testing.B) {
	compiled, err := Compile(document())
	if err != nil {
		b.Fatal(err)
	}
	attributes := expr.MapAttributes{
		"user.key":     expr.String("user-1234"),
		"user.country": expr.String("PL"),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		compiled.Evaluate("checkout-v2", attributes)
	}
}

func BenchmarkSnapshotLoad(b *testing.B) {
	registry := NewRegistry()
	compiled, err := Compile(document())
	if err != nil {
		b.Fatal(err)
	}
	registry.Publish(compiled)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = registry.Snapshot()
	}
}
