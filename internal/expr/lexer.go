package expr

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Lexer struct {
	input  string
	offset int
	line   int
	column int
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: input, line: 1, column: 1}
}

func (l *Lexer) Next() Token {
	l.skipSpaceAndComments()

	pos := l.position()
	if l.offset >= len(l.input) {
		return Token{Type: TokenEOF, Pos: pos}
	}

	r, width := l.peek()

	switch {
	case r == '"' || r == '\'':
		return l.readString(r)
	case unicode.IsDigit(r):
		return l.readNumber()
	case r == '_' || unicode.IsLetter(r):
		return l.readIdent()
	}

	l.advance(width)

	switch r {
	case '(':
		return Token{Type: TokenLParen, Literal: "(", Pos: pos}
	case ')':
		return Token{Type: TokenRParen, Literal: ")", Pos: pos}
	case '[':
		return Token{Type: TokenLBracket, Literal: "[", Pos: pos}
	case ']':
		return Token{Type: TokenRBracket, Literal: "]", Pos: pos}
	case ',':
		return Token{Type: TokenComma, Literal: ",", Pos: pos}
	case '=':
		if l.take('=') {
			return Token{Type: TokenEq, Literal: "==", Pos: pos}
		}
		return Token{Type: TokenIllegal, Literal: "=", Pos: pos}
	case '!':
		if l.take('=') {
			return Token{Type: TokenNotEq, Literal: "!=", Pos: pos}
		}
		return Token{Type: TokenNot, Literal: "!", Pos: pos}
	case '>':
		if l.take('=') {
			return Token{Type: TokenGte, Literal: ">=", Pos: pos}
		}
		return Token{Type: TokenGt, Literal: ">", Pos: pos}
	case '<':
		if l.take('=') {
			return Token{Type: TokenLte, Literal: "<=", Pos: pos}
		}
		return Token{Type: TokenLt, Literal: "<", Pos: pos}
	case '&':
		if l.take('&') {
			return Token{Type: TokenAnd, Literal: "&&", Pos: pos}
		}
		return Token{Type: TokenIllegal, Literal: "&", Pos: pos}
	case '|':
		if l.take('|') {
			return Token{Type: TokenOr, Literal: "||", Pos: pos}
		}
		return Token{Type: TokenIllegal, Literal: "|", Pos: pos}
	default:
		return Token{Type: TokenIllegal, Literal: string(r), Pos: pos}
	}
}

func (l *Lexer) Tokens() []Token {
	var tokens []Token
	for {
		token := l.Next()
		tokens = append(tokens, token)
		if token.Type == TokenEOF {
			return tokens
		}
	}
}

func (l *Lexer) readIdent() Token {
	pos := l.position()
	start := l.offset

	for l.offset < len(l.input) {
		r, width := l.peek()
		if r != '_' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		l.advance(width)
	}

	literal := l.input[start:l.offset]
	if keyword, ok := keywords[strings.ToLower(literal)]; ok {
		return Token{Type: keyword, Literal: literal, Pos: pos}
	}
	return Token{Type: TokenIdent, Literal: literal, Pos: pos}
}

func (l *Lexer) readNumber() Token {
	pos := l.position()
	start := l.offset
	seenDot := false

	for l.offset < len(l.input) {
		r, width := l.peek()
		if r == '.' {
			if seenDot {
				break
			}
			seenDot = true
			l.advance(width)
			continue
		}
		if !unicode.IsDigit(r) {
			break
		}
		l.advance(width)
	}

	return Token{Type: TokenNumber, Literal: l.input[start:l.offset], Pos: pos}
}

func (l *Lexer) readString(quote rune) Token {
	pos := l.position()
	l.advance(utf8.RuneLen(quote))

	var builder strings.Builder
	for l.offset < len(l.input) {
		r, width := l.peek()
		l.advance(width)

		switch r {
		case quote:
			return Token{Type: TokenString, Literal: builder.String(), Pos: pos}
		case '\\':
			if l.offset >= len(l.input) {
				return Token{Type: TokenIllegal, Literal: "unterminated string", Pos: pos}
			}
			escaped, escapedWidth := l.peek()
			l.advance(escapedWidth)
			switch escaped {
			case 'n':
				builder.WriteRune('\n')
			case 't':
				builder.WriteRune('\t')
			case '\\', '"', '\'':
				builder.WriteRune(escaped)
			default:
				builder.WriteRune('\\')
				builder.WriteRune(escaped)
			}
		default:
			builder.WriteRune(r)
		}
	}

	return Token{Type: TokenIllegal, Literal: "unterminated string", Pos: pos}
}

func (l *Lexer) skipSpaceAndComments() {
	for l.offset < len(l.input) {
		r, width := l.peek()
		if unicode.IsSpace(r) {
			l.advance(width)
			continue
		}
		if r == '#' {
			for l.offset < len(l.input) {
				c, w := l.peek()
				if c == '\n' {
					break
				}
				l.advance(w)
			}
			continue
		}
		return
	}
}

func (l *Lexer) peek() (rune, int) {
	return utf8.DecodeRuneInString(l.input[l.offset:])
}

func (l *Lexer) take(want rune) bool {
	if l.offset >= len(l.input) {
		return false
	}
	r, width := l.peek()
	if r != want {
		return false
	}
	l.advance(width)
	return true
}

func (l *Lexer) advance(width int) {
	if l.offset < len(l.input) && l.input[l.offset] == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
	l.offset += width
}

func (l *Lexer) position() Position {
	return Position{Offset: l.offset, Line: l.line, Column: l.column}
}
