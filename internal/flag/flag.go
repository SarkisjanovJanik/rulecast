package flag

import (
	"encoding/json"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/expr"
)

type Kind string

const (
	KindBool   Kind = "bool"
	KindString Kind = "string"
	KindNumber Kind = "number"
)

func (k Kind) Valid() bool {
	switch k {
	case KindBool, KindString, KindNumber:
		return true
	default:
		return false
	}
}

type Variation struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type RolloutEntry struct {
	Variation string  `json:"variation"`
	Weight    float64 `json:"weight"`
}

type Serve struct {
	Variation string         `json:"variation,omitempty"`
	Rollout   []RolloutEntry `json:"rollout,omitempty"`
}

func (s Serve) IsRollout() bool { return len(s.Rollout) > 0 }

type Rule struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Expression  string `json:"expression"`
	Serve       Serve  `json:"serve"`
}

type Flag struct {
	Key          string      `json:"key"`
	Description  string      `json:"description,omitempty"`
	Kind         Kind        `json:"kind"`
	Enabled      bool        `json:"enabled"`
	Salt         string      `json:"salt"`
	Variations   []Variation `json:"variations"`
	Default      string      `json:"default"`
	OffVariation string      `json:"offVariation"`
	Rules        []Rule      `json:"rules,omitempty"`
}

func (f Flag) Variation(key string) (Variation, bool) {
	for _, variation := range f.Variations {
		if variation.Key == key {
			return variation, true
		}
	}
	return Variation{}, false
}

type Document struct {
	Environment string            `json:"environment"`
	Version     int64             `json:"version"`
	CreatedAt   time.Time         `json:"createdAt"`
	CreatedBy   string            `json:"createdBy,omitempty"`
	Comment     string            `json:"comment,omitempty"`
	Schema      map[string]string `json:"schema"`
	Flags       []Flag            `json:"flags"`
}

func (d Document) Flag(key string) (Flag, bool) {
	for _, f := range d.Flags {
		if f.Key == key {
			return f, true
		}
	}
	return Flag{}, false
}

func (d Document) ExprSchema() (expr.Schema, error) {
	schema := make(expr.Schema, len(d.Schema))
	for attribute, kind := range d.Schema {
		switch Kind(kind) {
		case KindBool:
			schema[attribute] = expr.KindBool
		case KindString:
			schema[attribute] = expr.KindString
		case KindNumber:
			schema[attribute] = expr.KindNumber
		default:
			return nil, &ValidationError{Problems: []Problem{{
				Path:    "schema." + attribute,
				Message: "unknown attribute type " + kind,
			}}}
		}
	}
	return schema, nil
}

func (d Document) WithFlag(f Flag) Document {
	next := d
	next.Flags = make([]Flag, 0, len(d.Flags)+1)

	replaced := false
	for _, existing := range d.Flags {
		if existing.Key == f.Key {
			next.Flags = append(next.Flags, f)
			replaced = true
			continue
		}
		next.Flags = append(next.Flags, existing)
	}
	if !replaced {
		next.Flags = append(next.Flags, f)
	}
	return next
}

func (d Document) WithoutFlag(key string) Document {
	next := d
	next.Flags = make([]Flag, 0, len(d.Flags))
	for _, existing := range d.Flags {
		if existing.Key != key {
			next.Flags = append(next.Flags, existing)
		}
	}
	return next
}
