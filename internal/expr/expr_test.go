package expr

import (
	"strings"
	"testing"
)

func attributes() MapAttributes {
	return MapAttributes{
		"user.key":      String("u-4211"),
		"user.country":  String("DE"),
		"user.tier":     String("vip"),
		"user.email":    String("ada@example.com"),
		"user.language": String("de"),
		"user.age":      Number(34),
		"user.spend":    Number(250),
		"user.beta":     Bool(true),
		"device.type":   String("mobile"),
		"device.os":     String("ios"),
		"app.version":   String("4.12.0"),
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		rule string
		want bool
	}{
		{`user.country == "DE"`, true},
		{`user.country == "PL"`, false},
		{`user.country != "PL"`, true},
		{`user.country in ["DE", "PL", "AT"]`, true},
		{`user.country in ["PL", "AT"]`, false},
		{`user.spend > 100`, true},
		{`user.spend > 250`, false},
		{`user.spend >= 250`, true},
		{`user.age < 40 and user.tier == "vip"`, true},
		{`user.age < 30 and user.tier == "vip"`, false},
		{`user.age < 30 or user.tier == "vip"`, true},
		{`not user.beta`, false},
		{`user.beta`, true},
		{`not (user.country == "PL")`, true},
		{`user.email matches "^ada@"`, true},
		{`user.email matches "^bob@"`, false},
		{`user.email contains "@example"`, true},
		{`user.email contains "@gmail"`, false},
		{`user.email startsWith "ada"`, true},
		{`user.email startsWith "bob"`, false},
		{`user.email endsWith ".com"`, true},
		{`user.email endsWith ".net"`, false},
		{`user.email endsWith "@example.com" and user.tier == "vip"`, true},
		{`app.version startsWith "4."`, true},
		{`app.version startsWith "3."`, false},

		// Case matters; use matches with (?i) when it should not.
		{`user.email contains "ADA"`, false},
		{`user.email matches "(?i)ADA"`, true},
		{`user.country == "DE" and (user.tier == "vip" or user.spend > 1000)`, true},
		{`(user.country == "PL" or user.country == "DE") and not user.beta`, false},
		{`user.tier == "vip" and user.spend > 100 and device.type == "mobile"`, true},

		{`user.country == "DE" or user.tier == "none" and user.spend > 99999`, true},

		{`session.region == "eu"`, false},
	}

	schema := DefaultSchema()
	context := attributes()

	for _, test := range tests {
		predicate, _, err := Compile(test.rule, schema)
		if err != nil {
			t.Fatalf("Compile(%q) returned error: %v", test.rule, err)
		}
		if got := predicate(context); got != test.want {
			t.Errorf("%s = %v, want %v", test.rule, got, test.want)
		}
	}
}

func TestCompileRejects(t *testing.T) {
	tests := []struct {
		rule    string
		message string
	}{
		{`user.contry == "DE"`, "unknown attribute"},
		{`user.spend > "100"`, "expects numbers"},
		{`user.country > 5`, "expects numbers"},
		{`user.country == 5`, "cannot compare"},
		{`user.spend`, "must evaluate to bool"},
		{`user.country`, "must evaluate to bool"},
		{`user.age and user.beta`, "expects bool on both sides"},
		{`not user.age`, "'not' expects bool"},
		{`user.country in "DE"`, "expects a list"},
		{`user.spend contains "1"`, "contains expects strings"},
		{`user.email startsWith 5`, "startsWith expects strings"},
		{`user.email endsWith user.age`, "endsWith expects strings"},
		{`user.age in ["1"]`, "expects a string on the left"},
		{`user.email matches "("`, "invalid pattern"},
		{`user.country == `, "unexpected end of expression"},
		{`user.country == "DE" and`, "unexpected end of expression"},
		{`(user.country == "DE"`, "missing closing"},
		{`user.country in ["DE"`, "expected ',' or ']'"},
		{`user.country in [5]`, "list items must be strings"},
		{``, "expression is empty"},
		{`user.country = "DE"`, "unexpected input"},
		{`user.country == "DE" garbage`, "unexpected"},
	}

	schema := DefaultSchema()

	for _, test := range tests {
		_, _, err := Compile(test.rule, schema)
		if err == nil {
			t.Errorf("Compile(%q) succeeded, expected error containing %q", test.rule, test.message)
			continue
		}
		if !strings.Contains(err.Error(), test.message) {
			t.Errorf("Compile(%q) error = %q, want it to contain %q", test.rule, err.Error(), test.message)
		}
	}
}

func TestErrorPointsAtTheOffendingColumn(t *testing.T) {
	rule := `user.country == "DE" and user.contry == "PL"`

	_, _, err := Compile(rule, DefaultSchema())
	if err == nil {
		t.Fatal("expected an error")
	}

	parseError, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}

	annotated := parseError.Annotated()
	if !strings.Contains(annotated, rule) {
		t.Errorf("annotation should quote the source line, got:\n%s", annotated)
	}

	lines := strings.Split(annotated, "\n")
	caretLine := lines[len(lines)-1]
	caretAt := strings.Index(caretLine, "^")
	if caretAt != strings.Index(rule, "user.contry")+2 {
		t.Errorf("caret at %d, expected it under \"user.contry\"\n%s", caretAt, annotated)
	}
}

func TestUnquotedStringsAndComments(t *testing.T) {
	predicate, _, err := Compile(`user.tier == 'vip' # only paying users`, DefaultSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !predicate(attributes()) {
		t.Error("expected the rule to match")
	}
}

func TestInSwitchesToASetForLongLists(t *testing.T) {
	var items []string
	for i := range setThreshold + 5 {
		items = append(items, "country-"+string(rune('a'+i)))
	}
	items = append(items, "DE")

	rule := `user.country in ["` + strings.Join(items, `", "`) + `"]`
	predicate, _, err := Compile(rule, DefaultSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !predicate(attributes()) {
		t.Error("expected a match through the set path")
	}
}

func BenchmarkEvaluateSimple(b *testing.B) {
	predicate, _, err := Compile(`user.country == "DE"`, DefaultSchema())
	if err != nil {
		b.Fatal(err)
	}
	context := attributes()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !predicate(context) {
			b.Fatal("expected a match")
		}
	}
}

func BenchmarkEvaluateComplex(b *testing.B) {
	rule := `user.country in ["DE", "AT", "CH"] and user.tier == "vip" ` +
		`and user.spend > 100 and not (device.type == "desktop") ` +
		`and (app.version matches "^4\\." or user.beta)`

	predicate, _, err := Compile(rule, DefaultSchema())
	if err != nil {
		b.Fatal(err)
	}
	context := attributes()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !predicate(context) {
			b.Fatal("expected a match")
		}
	}
}

func BenchmarkCompile(b *testing.B) {
	rule := `user.country in ["DE", "AT", "CH"] and user.tier == "vip" and user.spend > 100`
	schema := DefaultSchema()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := Compile(rule, schema); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzCompile(f *testing.F) {
	seeds := []string{
		`user.country == "DE"`,
		`user.spend > 100 and user.tier in ["vip", "gold"]`,
		`not (user.beta or user.age < 18)`,
		`user.email matches "^a"`,
		`user.email endsWith "@example.com"`,
		`app.version startsWith "4."`,
		`[`,
		`((((`,
		`"unterminated`,
		`user.country == == "DE"`,
		`1.2.3.4`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	schema := DefaultSchema()
	f.Fuzz(func(t *testing.T, rule string) {
		predicate, _, err := Compile(rule, schema)
		if err != nil {
			return
		}
		predicate(attributes())
	})
}
