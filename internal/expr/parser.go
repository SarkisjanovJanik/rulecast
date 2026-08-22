package expr

import "strconv"

const (
	precLowest = iota
	precOr
	precAnd
	precCompare
	precUnary
)

var precedences = map[TokenType]int{
	TokenOr:      precOr,
	TokenAnd:     precAnd,
	TokenEq:      precCompare,
	TokenNotEq:   precCompare,
	TokenGt:      precCompare,
	TokenGte:     precCompare,
	TokenLt:      precCompare,
	TokenLte:     precCompare,
	TokenIn:      precCompare,
	TokenMatches:    precCompare,
	TokenContains:   precCompare,
	TokenStartsWith: precCompare,
	TokenEndsWith:   precCompare,
}

type Parser struct {
	input   string
	lexer   *Lexer
	current Token
	next    Token
}

func Parse(input string) (Node, error) {
	parser := &Parser{input: input, lexer: NewLexer(input)}
	parser.advance()
	parser.advance()

	if parser.current.Type == TokenEOF {
		return nil, newError(input, "expression is empty", parser.current.Pos)
	}

	node, err := parser.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	if parser.current.Type != TokenEOF {
		return nil, parser.unexpected(parser.current)
	}
	return node, nil
}

func (p *Parser) advance() {
	p.current = p.next
	p.next = p.lexer.Next()
}

func (p *Parser) parseExpression(precedence int) (Node, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}

	for {
		operator := p.current
		operatorPrecedence, ok := precedences[operator.Type]
		if !ok || operatorPrecedence <= precedence {
			return left, nil
		}
		p.advance()

		right, err := p.parseOperand(operator, operatorPrecedence)
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: operator.Type, Left: left, Right: right, Position: operator.Pos}
	}
}

func (p *Parser) parseOperand(operator Token, precedence int) (Node, error) {
	switch operator.Type {
	case TokenIn:
		if p.current.Type != TokenLBracket {
			return nil, newError(p.input, "'in' expects a list, got "+p.current.Type.String(), p.current.Pos)
		}
		return p.parseList()
	case TokenMatches:
		if p.current.Type != TokenString {
			return nil, newError(p.input,
				"'matches' expects a pattern string, got "+p.current.Type.String(), p.current.Pos)
		}
		return p.parsePrefix()
	default:
		return p.parseExpression(precedence)
	}
}

func (p *Parser) parsePrefix() (Node, error) {
	token := p.current

	switch token.Type {
	case TokenNot:
		p.advance()
		operand, err := p.parseExpression(precUnary)
		if err != nil {
			return nil, err
		}
		return &NotExpr{Operand: operand, Position: token.Pos}, nil

	case TokenLParen:
		p.advance()
		inner, err := p.parseExpression(precLowest)
		if err != nil {
			return nil, err
		}
		if p.current.Type != TokenRParen {
			return nil, newError(p.input, "missing closing ')'", p.current.Pos)
		}
		p.advance()
		return inner, nil

	case TokenLBracket:
		return p.parseList()

	case TokenIdent:
		p.advance()
		return &Attribute{Name: token.Literal, Position: token.Pos}, nil

	case TokenString:
		p.advance()
		return &StringLiteral{Value: token.Literal, Position: token.Pos}, nil

	case TokenNumber:
		p.advance()
		number, err := strconv.ParseFloat(token.Literal, 64)
		if err != nil {
			return nil, newError(p.input, "invalid number "+token.Literal, token.Pos)
		}
		return &NumberLiteral{Value: number, Position: token.Pos}, nil

	case TokenTrue, TokenFalse:
		p.advance()
		return &BoolLiteral{Value: token.Type == TokenTrue, Position: token.Pos}, nil

	case TokenIllegal:
		return nil, p.unexpected(token)

	case TokenEOF:
		return nil, newError(p.input, "unexpected end of expression", token.Pos)

	default:
		return nil, newError(p.input, "unexpected "+token.Type.String(), token.Pos)
	}
}

func (p *Parser) unexpected(token Token) error {
	if token.Type == TokenIllegal {
		return newError(p.input, "unexpected input "+strconv.Quote(token.Literal), token.Pos)
	}
	return newError(p.input, "unexpected "+token.Type.String(), token.Pos)
}

func (p *Parser) parseList() (Node, error) {
	open := p.current
	p.advance()

	items := make([]string, 0, 4)
	for p.current.Type != TokenRBracket {
		if p.current.Type != TokenString {
			return nil, newError(p.input,
				"list items must be strings, got "+p.current.Type.String(), p.current.Pos)
		}
		items = append(items, p.current.Literal)
		p.advance()

		if p.current.Type == TokenComma {
			p.advance()
			continue
		}
		if p.current.Type != TokenRBracket {
			return nil, newError(p.input, "expected ',' or ']'", p.current.Pos)
		}
	}
	p.advance()

	return &ListLiteral{Items: items, Position: open.Pos}, nil
}
