package expr

import "sort"

type Schema map[string]Kind

func (s Schema) Kind(attribute string) (Kind, bool) {
	kind, ok := s[attribute]
	return kind, ok
}

func (s Schema) Names() []string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func DefaultSchema() Schema {
	return Schema{
		"user.key":       KindString,
		"user.country":   KindString,
		"user.tier":      KindString,
		"user.email":     KindString,
		"user.language":  KindString,
		"user.age":       KindNumber,
		"user.spend":     KindNumber,
		"user.beta":      KindBool,
		"device.type":    KindString,
		"device.os":      KindString,
		"app.version":    KindString,
		"session.region": KindString,
	}
}
