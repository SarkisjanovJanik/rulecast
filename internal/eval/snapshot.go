package eval

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jsarkisjanov/rulecast/internal/bucket"
	"github.com/jsarkisjanov/rulecast/internal/expr"
	"github.com/jsarkisjanov/rulecast/internal/flag"
)

const AttributeUserKey = "user.key"

type Reason string

const (
	ReasonFlagNotFound Reason = "FLAG_NOT_FOUND"
	ReasonFlagOff      Reason = "FLAG_OFF"
	ReasonRuleMatch    Reason = "RULE_MATCH"
	ReasonRollout      Reason = "ROLLOUT"
	ReasonDefault      Reason = "DEFAULT"
	ReasonMissingKey   Reason = "MISSING_USER_KEY"
)

type Result struct {
	FlagKey   string          `json:"flagKey"`
	Variation string          `json:"variation,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	Reason    Reason          `json:"reason"`
	RuleID    string          `json:"ruleId,omitempty"`
	Version   int64           `json:"version"`
}

type compiledRule struct {
	id        string
	predicate expr.Predicate
	variation string
	rollout   []bucket.Weighted
}

type compiledFlag struct {
	key        string
	enabled    bool
	salt       string
	values     map[string]json.RawMessage
	defaultKey string
	offKey     string
	rules      []compiledRule
}

type Snapshot struct {
	environment string
	version     int64
	flags       map[string]*compiledFlag
	keys        []string
	document    flag.Document
}

func Compile(document flag.Document) (*Snapshot, error) {
	if err := document.Validate(); err != nil {
		return nil, fmt.Errorf("validate document: %w", err)
	}

	schema, err := document.ExprSchema()
	if err != nil {
		return nil, fmt.Errorf("build schema: %w", err)
	}

	snapshot := &Snapshot{
		environment: document.Environment,
		version:     document.Version,
		flags:       make(map[string]*compiledFlag, len(document.Flags)),
		keys:        make([]string, 0, len(document.Flags)),
		document:    document,
	}

	for _, f := range document.Flags {
		compiled, err := compileFlag(f, schema)
		if err != nil {
			return nil, fmt.Errorf("flag %q: %w", f.Key, err)
		}
		snapshot.flags[f.Key] = compiled
		snapshot.keys = append(snapshot.keys, f.Key)
	}
	sort.Strings(snapshot.keys)

	return snapshot, nil
}

func compileFlag(f flag.Flag, schema expr.Schema) (*compiledFlag, error) {
	compiled := &compiledFlag{
		key:        f.Key,
		enabled:    f.Enabled,
		salt:       f.Salt,
		values:     make(map[string]json.RawMessage, len(f.Variations)),
		defaultKey: f.Default,
		offKey:     f.OffVariation,
		rules:      make([]compiledRule, 0, len(f.Rules)),
	}

	for _, variation := range f.Variations {
		compiled.values[variation.Key] = variation.Value
	}

	for _, rule := range f.Rules {
		predicate, _, err := expr.Compile(rule.Expression, schema)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.ID, err)
		}

		entry := compiledRule{id: rule.ID, predicate: predicate, variation: rule.Serve.Variation}
		for _, weighted := range rule.Serve.Rollout {
			entry.rollout = append(entry.rollout, bucket.Weighted{
				Key:    weighted.Variation,
				Weight: weighted.Weight,
			})
		}
		compiled.rules = append(compiled.rules, entry)
	}

	return compiled, nil
}

func (s *Snapshot) Environment() string     { return s.environment }
func (s *Snapshot) Version() int64          { return s.version }
func (s *Snapshot) Keys() []string          { return s.keys }
func (s *Snapshot) Document() flag.Document { return s.document }

func (s *Snapshot) Evaluate(flagKey string, attributes expr.Attributes) Result {
	compiled, ok := s.flags[flagKey]
	if !ok {
		return Result{FlagKey: flagKey, Reason: ReasonFlagNotFound, Version: s.version}
	}

	if !compiled.enabled {
		return s.serve(compiled, compiled.offKey, ReasonFlagOff, "")
	}

	for i := range compiled.rules {
		rule := &compiled.rules[i]
		if !rule.predicate(attributes) {
			continue
		}

		if rule.variation != "" {
			return s.serve(compiled, rule.variation, ReasonRuleMatch, rule.id)
		}

		userKey := attributes.Lookup(AttributeUserKey)
		if userKey.Kind() != expr.KindString || userKey.Str() == "" {
			return Result{
				FlagKey: compiled.key,
				Reason:  ReasonMissingKey,
				RuleID:  rule.id,
				Version: s.version,
			}
		}

		position := bucket.Of(compiled.key, compiled.salt, userKey.Str())
		if variation, ok := bucket.Pick(position, rule.rollout); ok {
			return s.serve(compiled, variation, ReasonRollout, rule.id)
		}
		return s.serve(compiled, compiled.defaultKey, ReasonDefault, rule.id)
	}

	return s.serve(compiled, compiled.defaultKey, ReasonDefault, "")
}

func (s *Snapshot) EvaluateAll(attributes expr.Attributes) []Result {
	results := make([]Result, 0, len(s.keys))
	for _, key := range s.keys {
		results = append(results, s.Evaluate(key, attributes))
	}
	return results
}

func (s *Snapshot) serve(compiled *compiledFlag, variation string, reason Reason, ruleID string) Result {
	return Result{
		FlagKey:   compiled.key,
		Variation: variation,
		Value:     compiled.values[variation],
		Reason:    reason,
		RuleID:    ruleID,
		Version:   s.version,
	}
}
