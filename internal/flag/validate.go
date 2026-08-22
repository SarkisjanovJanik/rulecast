package flag

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"

	"github.com/jsarkisjanov/rulecast/internal/expr"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func (d Document) Validate() error {
	problems := &ValidationError{}

	if !keyPattern.MatchString(d.Environment) {
		problems.add("environment", "must match %s", keyPattern)
	}
	if d.Version < 1 {
		problems.add("version", "must be positive")
	}

	schema, err := d.ExprSchema()
	if err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			problems.Problems = append(problems.Problems, validation.Problems...)
		} else {
			problems.add("schema", "%v", err)
		}
		return problems.orNil()
	}

	seen := make(map[string]struct{}, len(d.Flags))
	for i, f := range d.Flags {
		path := fmt.Sprintf("flags[%d]", i)
		if _, duplicate := seen[f.Key]; duplicate {
			problems.add(path+".key", "duplicate flag key %q", f.Key)
		}
		seen[f.Key] = struct{}{}

		f.validate(path, schema, problems)
	}

	return problems.orNil()
}

func (f Flag) validate(path string, schema expr.Schema, problems *ValidationError) {
	if !keyPattern.MatchString(f.Key) {
		problems.add(path+".key", "must match %s", keyPattern)
	}
	if !f.Kind.Valid() {
		problems.add(path+".kind", "must be bool, string or number")
	}
	if f.Salt == "" {
		problems.add(path+".salt", "must not be empty; it decorrelates this flag's buckets")
	}
	if len(f.Variations) < 1 {
		problems.add(path+".variations", "at least one variation is required")
		return
	}

	keys := make(map[string]struct{}, len(f.Variations))
	for i, variation := range f.Variations {
		variationPath := fmt.Sprintf("%s.variations[%d]", path, i)
		if variation.Key == "" {
			problems.add(variationPath+".key", "must not be empty")
		}
		if _, duplicate := keys[variation.Key]; duplicate {
			problems.add(variationPath+".key", "duplicate variation key %q", variation.Key)
		}
		keys[variation.Key] = struct{}{}

		if err := checkValueKind(variation.Value, f.Kind); err != nil {
			problems.add(variationPath+".value", "%v", err)
		}
	}

	if _, ok := keys[f.Default]; !ok {
		problems.add(path+".default", "unknown variation %q", f.Default)
	}
	if _, ok := keys[f.OffVariation]; !ok {
		problems.add(path+".offVariation", "unknown variation %q", f.OffVariation)
	}

	ruleIDs := make(map[string]struct{}, len(f.Rules))
	for i, rule := range f.Rules {
		rulePath := fmt.Sprintf("%s.rules[%d]", path, i)
		if rule.ID == "" {
			problems.add(rulePath+".id", "must not be empty")
		}
		if _, duplicate := ruleIDs[rule.ID]; duplicate {
			problems.add(rulePath+".id", "duplicate rule id %q", rule.ID)
		}
		ruleIDs[rule.ID] = struct{}{}

		if _, _, err := expr.Compile(rule.Expression, schema); err != nil {
			problems.add(rulePath+".expression", "%v", err)
		}

		rule.Serve.validate(rulePath+".serve", keys, problems)
	}
}

func (s Serve) validate(path string, variations map[string]struct{}, problems *ValidationError) {
	switch {
	case s.Variation != "" && s.IsRollout():
		problems.add(path, "set either variation or rollout, not both")
	case s.Variation != "":
		if _, ok := variations[s.Variation]; !ok {
			problems.add(path+".variation", "unknown variation %q", s.Variation)
		}
	case s.IsRollout():
		var total float64
		for i, entry := range s.Rollout {
			entryPath := fmt.Sprintf("%s.rollout[%d]", path, i)
			if _, ok := variations[entry.Variation]; !ok {
				problems.add(entryPath+".variation", "unknown variation %q", entry.Variation)
			}
			if entry.Weight < 0 {
				problems.add(entryPath+".weight", "must not be negative")
			}
			total += entry.Weight
		}
		if total <= 0 {
			problems.add(path+".rollout", "weights must add up to more than zero")
		}
	default:
		problems.add(path, "must serve a variation or a rollout")
	}
}

func checkValueKind(raw json.RawMessage, kind Kind) error {
	if len(raw) == 0 {
		return fmt.Errorf("must not be empty")
	}

	switch kind {
	case KindBool:
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("must be a bool")
		}
	case KindString:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("must be a string")
		}
	case KindNumber:
		var value float64
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("must be a number")
		}
		if math.IsInf(value, 0) || math.IsNaN(value) {
			return fmt.Errorf("must be finite")
		}
	default:
		return fmt.Errorf("flag kind is invalid")
	}
	return nil
}
