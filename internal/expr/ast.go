package expr

import "strings"

type Node interface {
	Pos() Position
	String() string
}

type BoolLiteral struct {
	Value    bool
	Position Position
}

type NumberLiteral struct {
	Value    float64
	Position Position
}

type StringLiteral struct {
	Value    string
	Position Position
}

type ListLiteral struct {
	Items    []string
	Position Position
}

type Attribute struct {
	Name     string
	Position Position
}

type NotExpr struct {
	Operand  Node
	Position Position
}

type BinaryExpr struct {
	Op       TokenType
	Left     Node
	Right    Node
	Position Position
}

func (n *BoolLiteral) Pos() Position   { return n.Position }
func (n *NumberLiteral) Pos() Position { return n.Position }
func (n *StringLiteral) Pos() Position { return n.Position }
func (n *ListLiteral) Pos() Position   { return n.Position }
func (n *Attribute) Pos() Position     { return n.Position }
func (n *NotExpr) Pos() Position       { return n.Position }
func (n *BinaryExpr) Pos() Position    { return n.Position }

func (n *BoolLiteral) String() string   { return Bool(n.Value).String() }
func (n *NumberLiteral) String() string { return Number(n.Value).String() }
func (n *StringLiteral) String() string { return String(n.Value).String() }
func (n *Attribute) String() string     { return n.Name }

func (n *ListLiteral) String() string {
	quoted := make([]string, len(n.Items))
	for i, item := range n.Items {
		quoted[i] = String(item).String()
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func (n *NotExpr) String() string {
	return "not " + n.Operand.String()
}

func (n *BinaryExpr) String() string {
	return "(" + n.Left.String() + " " + n.Op.String() + " " + n.Right.String() + ")"
}
