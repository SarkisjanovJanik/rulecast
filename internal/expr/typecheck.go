package expr

import (
	"regexp"
	"strconv"
)

type checker struct {
	input  string
	schema Schema
}

func TypeCheck(input string, node Node, schema Schema) error {
	c := &checker{input: input, schema: schema}
	kind, err := c.check(node)
	if err != nil {
		return err
	}
	if kind != KindBool {
		return newError(input, "a rule must evaluate to bool, got "+kind.String(), node.Pos())
	}
	return nil
}

func (c *checker) check(node Node) (Kind, error) {
	switch n := node.(type) {
	case *BoolLiteral:
		return KindBool, nil
	case *NumberLiteral:
		return KindNumber, nil
	case *StringLiteral:
		return KindString, nil
	case *ListLiteral:
		return KindStringList, nil

	case *Attribute:
		kind, ok := c.schema.Kind(n.Name)
		if !ok {
			return KindInvalid, newError(c.input, "unknown attribute "+strconv.Quote(n.Name), n.Position)
		}
		return kind, nil

	case *NotExpr:
		kind, err := c.check(n.Operand)
		if err != nil {
			return KindInvalid, err
		}
		if kind != KindBool {
			return KindInvalid, newError(c.input, "'not' expects bool, got "+kind.String(), n.Position)
		}
		return KindBool, nil

	case *BinaryExpr:
		return c.checkBinary(n)

	default:
		return KindInvalid, newError(c.input, "unsupported expression", node.Pos())
	}
}

func (c *checker) checkBinary(n *BinaryExpr) (Kind, error) {
	left, err := c.check(n.Left)
	if err != nil {
		return KindInvalid, err
	}
	right, err := c.check(n.Right)
	if err != nil {
		return KindInvalid, err
	}

	switch n.Op {
	case TokenAnd, TokenOr:
		if left != KindBool || right != KindBool {
			return KindInvalid, newError(c.input,
				n.Op.String()+" expects bool on both sides, got "+left.String()+" and "+right.String(),
				n.Position)
		}
		return KindBool, nil

	case TokenEq, TokenNotEq:
		if left != right {
			return KindInvalid, newError(c.input,
				"cannot compare "+left.String()+" with "+right.String(), n.Position)
		}
		if left == KindStringList {
			return KindInvalid, newError(c.input, "lists are not comparable", n.Position)
		}
		return KindBool, nil

	case TokenGt, TokenGte, TokenLt, TokenLte:
		if left != KindNumber || right != KindNumber {
			return KindInvalid, newError(c.input,
				n.Op.String()+" expects numbers, got "+left.String()+" and "+right.String(), n.Position)
		}
		return KindBool, nil

	case TokenIn:
		if left != KindString {
			return KindInvalid, newError(c.input,
				"'in' expects a string on the left, got "+left.String(), n.Position)
		}
		if right != KindStringList {
			return KindInvalid, newError(c.input,
				"'in' expects a list on the right, got "+right.String(), n.Position)
		}
		return KindBool, nil

	case TokenContains, TokenStartsWith, TokenEndsWith:
		if left != KindString || right != KindString {
			return KindInvalid, newError(c.input,
				n.Op.String()+" expects strings, got "+left.String()+" and "+right.String(), n.Position)
		}
		return KindBool, nil

	case TokenMatches:
		if left != KindString {
			return KindInvalid, newError(c.input,
				"'matches' expects a string on the left, got "+left.String(), n.Position)
		}
		pattern, ok := n.Right.(*StringLiteral)
		if !ok {
			return KindInvalid, newError(c.input, "'matches' expects a literal pattern", n.Position)
		}
		if _, err := regexp.Compile(pattern.Value); err != nil {
			return KindInvalid, newError(c.input, "invalid pattern: "+err.Error(), pattern.Position)
		}
		return KindBool, nil

	default:
		return KindInvalid, newError(c.input, "unsupported operator "+n.Op.String(), n.Position)
	}
}
