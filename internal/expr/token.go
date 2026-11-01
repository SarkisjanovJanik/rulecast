package expr

type TokenType uint8

const (
	TokenEOF TokenType = iota
	TokenIllegal
	TokenIdent
	TokenNumber
	TokenString
	TokenTrue
	TokenFalse
	TokenAnd
	TokenOr
	TokenNot
	TokenEq
	TokenNotEq
	TokenGt
	TokenGte
	TokenLt
	TokenLte
	TokenIn
	TokenMatches
	TokenContains
	TokenStartsWith
	TokenEndsWith
	TokenLParen
	TokenRParen
	TokenLBracket
	TokenRBracket
	TokenComma
)

var tokenNames = map[TokenType]string{
	TokenEOF:        "end of input",
	TokenIllegal:    "illegal token",
	TokenIdent:      "identifier",
	TokenNumber:     "number",
	TokenString:     "string",
	TokenTrue:       "true",
	TokenFalse:      "false",
	TokenAnd:        "and",
	TokenOr:         "or",
	TokenNot:        "not",
	TokenEq:         "==",
	TokenNotEq:      "!=",
	TokenGt:         ">",
	TokenGte:        ">=",
	TokenLt:         "<",
	TokenLte:        "<=",
	TokenIn:         "in",
	TokenMatches:    "matches",
	TokenContains:   "contains",
	TokenStartsWith: "startsWith",
	TokenEndsWith:   "endsWith",
	TokenLParen:     "(",
	TokenRParen:     ")",
	TokenLBracket:   "[",
	TokenRBracket:   "]",
	TokenComma:      ",",
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return "unknown"
}

var keywords = map[string]TokenType{
	"and":        TokenAnd,
	"or":         TokenOr,
	"not":        TokenNot,
	"in":         TokenIn,
	"matches":    TokenMatches,
	"contains":   TokenContains,
	"startswith": TokenStartsWith,
	"endswith":   TokenEndsWith,
	"true":       TokenTrue,
	"false":      TokenFalse,
}

type Position struct {
	Offset int
	Line   int
	Column int
}

type Token struct {
	Type    TokenType
	Literal string
	Pos     Position
}
