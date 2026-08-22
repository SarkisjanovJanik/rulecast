package expr

import (
	"regexp"
	"strings"
)

type Predicate func(Attributes) bool

type evaluator func(Attributes) Value

const setThreshold = 8

func Compile(input string, schema Schema) (Predicate, Node, error) {
	node, err := Parse(input)
	if err != nil {
		return nil, nil, err
	}
	if err := TypeCheck(input, node, schema); err != nil {
		return nil, nil, err
	}

	eval := compileNode(node)
	return func(attributes Attributes) bool {
		value := eval(attributes)
		return value.kind == KindBool && value.Bool()
	}, node, nil
}

func compileNode(node Node) evaluator {
	switch n := node.(type) {
	case *BoolLiteral:
		value := Bool(n.Value)
		return func(Attributes) Value { return value }

	case *NumberLiteral:
		value := Number(n.Value)
		return func(Attributes) Value { return value }

	case *StringLiteral:
		value := String(n.Value)
		return func(Attributes) Value { return value }

	case *ListLiteral:
		value := StringList(n.Items)
		return func(Attributes) Value { return value }

	case *Attribute:
		name := n.Name
		return func(attributes Attributes) Value { return attributes.Lookup(name) }

	case *NotExpr:
		operand := compileNode(n.Operand)
		return func(attributes Attributes) Value {
			value := operand(attributes)
			if value.kind != KindBool {
				return Invalid()
			}
			return Bool(!value.Bool())
		}

	case *BinaryExpr:
		return compileBinary(n)

	default:
		return func(Attributes) Value { return Invalid() }
	}
}

func compileBinary(n *BinaryExpr) evaluator {
	left := compileNode(n.Left)

	switch n.Op {
	case TokenAnd:
		right := compileNode(n.Right)
		return func(attributes Attributes) Value {
			if value := left(attributes); value.kind != KindBool || !value.Bool() {
				return Bool(false)
			}
			value := right(attributes)
			return Bool(value.kind == KindBool && value.Bool())
		}

	case TokenOr:
		right := compileNode(n.Right)
		return func(attributes Attributes) Value {
			if value := left(attributes); value.kind == KindBool && value.Bool() {
				return Bool(true)
			}
			value := right(attributes)
			return Bool(value.kind == KindBool && value.Bool())
		}

	case TokenIn:
		return compileIn(left, n.Right)

	case TokenContains:
		return compileSubstring(left, compileNode(n.Right), strings.Contains)

	case TokenStartsWith:
		return compileSubstring(left, compileNode(n.Right), strings.HasPrefix)

	case TokenEndsWith:
		return compileSubstring(left, compileNode(n.Right), strings.HasSuffix)

	case TokenMatches:
		pattern := regexp.MustCompile(n.Right.(*StringLiteral).Value)
		return func(attributes Attributes) Value {
			value := left(attributes)
			if value.kind != KindString {
				return Bool(false)
			}
			return Bool(pattern.MatchString(value.str))
		}
	}

	right := compileNode(n.Right)

	switch n.Op {
	case TokenEq:
		return func(attributes Attributes) Value {
			return Bool(equal(left(attributes), right(attributes)))
		}
	case TokenNotEq:
		return func(attributes Attributes) Value {
			a, b := left(attributes), right(attributes)
			if !a.IsValid() || !b.IsValid() {
				return Bool(false)
			}
			return Bool(!equal(a, b))
		}
	case TokenGt:
		return compareNumbers(left, right, func(a, b float64) bool { return a > b })
	case TokenGte:
		return compareNumbers(left, right, func(a, b float64) bool { return a >= b })
	case TokenLt:
		return compareNumbers(left, right, func(a, b float64) bool { return a < b })
	case TokenLte:
		return compareNumbers(left, right, func(a, b float64) bool { return a <= b })
	}

	return func(Attributes) Value { return Invalid() }
}

func compileIn(left evaluator, right Node) evaluator {
	items := right.(*ListLiteral).Items

	if len(items) > setThreshold {
		set := make(map[string]struct{}, len(items))
		for _, item := range items {
			set[item] = struct{}{}
		}
		return func(attributes Attributes) Value {
			value := left(attributes)
			if value.kind != KindString {
				return Bool(false)
			}
			_, found := set[value.str]
			return Bool(found)
		}
	}

	return func(attributes Attributes) Value {
		value := left(attributes)
		if value.kind != KindString {
			return Bool(false)
		}
		for _, item := range items {
			if item == value.str {
				return Bool(true)
			}
		}
		return Bool(false)
	}
}

func compileSubstring(left, right evaluator, match func(string, string) bool) evaluator {
	return func(attributes Attributes) Value {
		a, b := left(attributes), right(attributes)
		if a.kind != KindString || b.kind != KindString {
			return Bool(false)
		}
		return Bool(match(a.str, b.str))
	}
}

func compareNumbers(left, right evaluator, compare func(a, b float64) bool) evaluator {
	return func(attributes Attributes) Value {
		a, b := left(attributes), right(attributes)
		if a.kind != KindNumber || b.kind != KindNumber {
			return Bool(false)
		}
		return Bool(compare(a.num, b.num))
	}
}

func equal(a, b Value) bool {
	if a.kind != b.kind || !a.IsValid() {
		return false
	}
	switch a.kind {
	case KindString:
		return a.str == b.str
	case KindNumber:
		return a.num == b.num
	case KindBool:
		return a.Bool() == b.Bool()
	default:
		return false
	}
}
