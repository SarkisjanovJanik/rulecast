package expr

import (
	"strconv"
	"strings"
)

type Kind uint8

const (
	KindInvalid Kind = iota
	KindBool
	KindNumber
	KindString
	KindStringList
)

func (k Kind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindStringList:
		return "list<string>"
	default:
		return "invalid"
	}
}

type Value struct {
	kind Kind
	num  float64
	str  string
	list []string
}

func Bool(b bool) Value {
	if b {
		return Value{kind: KindBool, num: 1}
	}
	return Value{kind: KindBool}
}

func Number(n float64) Value { return Value{kind: KindNumber, num: n} }
func String(s string) Value  { return Value{kind: KindString, str: s} }
func Invalid() Value         { return Value{} }

func StringList(items []string) Value {
	return Value{kind: KindStringList, list: items}
}

func (v Value) Kind() Kind      { return v.kind }
func (v Value) IsValid() bool   { return v.kind != KindInvalid }
func (v Value) Bool() bool      { return v.num != 0 }
func (v Value) Number() float64 { return v.num }
func (v Value) Str() string     { return v.str }
func (v Value) List() []string  { return v.list }

func (v Value) String() string {
	switch v.kind {
	case KindBool:
		return strconv.FormatBool(v.Bool())
	case KindNumber:
		return strconv.FormatFloat(v.num, 'g', -1, 64)
	case KindString:
		return strconv.Quote(v.str)
	case KindStringList:
		return "[" + strings.Join(v.list, ", ") + "]"
	default:
		return "<invalid>"
	}
}

type Attributes interface {
	Lookup(key string) Value
}

type MapAttributes map[string]Value

func (m MapAttributes) Lookup(key string) Value { return m[key] }
