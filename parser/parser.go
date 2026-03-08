package parser

import (
	"fmt"
	"lux/ast"
	"lux/lexer"
	"strconv"
)

// Parser turns a token slice into an AST.
type Parser struct {
	tokens []lexer.Token
	pos    int
}

// New creates a parser from a token slice.
func New(tokens []lexer.Token) *Parser {
	return &Parser{tokens: tokens}
}

// Parse returns the root Program node or an error.
func (p *Parser) Parse() (*ast.Program, error) {
	prog := &ast.Program{}
	for !p.check(lexer.EOF) {
		p.skipNewlines()
		if p.check(lexer.EOF) {
			break
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		prog.Statements = append(prog.Statements, stmt)
		p.skipNewlines()
	}
	return prog, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (p *Parser) peek() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Type: lexer.EOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekAhead(n int) lexer.Token {
	idx := p.pos + n
	if idx >= len(p.tokens) {
		return lexer.Token{Type: lexer.EOF}
	}
	return p.tokens[idx]
}

func (p *Parser) advance() lexer.Token {
	t := p.peek()
	p.pos++
	return t
}

func (p *Parser) check(tt lexer.TokenType) bool { return p.peek().Type == tt }

func (p *Parser) expect(tt lexer.TokenType) (lexer.Token, error) {
	t := p.peek()
	if t.Type != tt {
		return t, fmt.Errorf("line %d: expected %s, got %s (%q)", t.Line, tt, t.Type, t.Literal)
	}
	return p.advance(), nil
}

func (p *Parser) skipNewlines() {
	for p.check(lexer.NEWLINE) {
		p.advance()
	}
}

func (p *Parser) expectNewlineOrEOF() error {
	t := p.peek()
	if t.Type == lexer.NEWLINE || t.Type == lexer.EOF || t.Type == lexer.RBRACE {
		if t.Type == lexer.NEWLINE {
			p.advance()
		}
		return nil
	}
	return fmt.Errorf("line %d: expected end of statement, got %s (%q)", t.Line, t.Type, t.Literal)
}

// ─── statements ──────────────────────────────────────────────────────────────

func (p *Parser) parseStatement() (ast.Statement, error) {
	switch p.peek().Type {
	case lexer.LET:
		return p.parseLetStmt()
	case lexer.IF:
		return p.parseIfStmt()
	case lexer.FOR:
		return p.parseForRangeStmt()
	case lexer.PRINT:
		return p.parsePrintStmt()
	case lexer.IDENT:
		// Could be assignment: name = expr
		if p.peekAhead(1).Type == lexer.ASSIGN {
			return p.parseAssignStmt()
		}
		return p.parseExprStmt()
	default:
		return p.parseExprStmt()
	}
}

func (p *Parser) parseLetStmt() (*ast.LetStmt, error) {
	line := p.peek().Line
	p.advance() // consume 'let'
	nameTok, err := p.expect(lexer.IDENT)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.ASSIGN); err != nil {
		return nil, err
	}
	val, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expectNewlineOrEOF(); err != nil {
		return nil, err
	}
	return &ast.LetStmt{Line: line, Name: nameTok.Literal, Value: val}, nil
}

func (p *Parser) parseAssignStmt() (*ast.AssignStmt, error) {
	nameTok := p.advance()
	p.advance() // consume '='
	val, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expectNewlineOrEOF(); err != nil {
		return nil, err
	}
	return &ast.AssignStmt{Line: nameTok.Line, Name: nameTok.Literal, Value: val}, nil
}

func (p *Parser) parseIfStmt() (*ast.IfStmt, error) {
	line := p.peek().Line
	p.advance() // consume 'if'
	cond, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	consequence, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	var alternative []ast.Statement
	p.skipNewlines()
	if p.check(lexer.ELSE) {
		p.advance()
		alternative, err = p.parseBlock()
		if err != nil {
			return nil, err
		}
	}
	return &ast.IfStmt{
		Line:        line,
		Condition:   cond,
		Consequence: consequence,
		Alternative: alternative,
	}, nil
}

func (p *Parser) parseForRangeStmt() (*ast.ForRangeStmt, error) {
	line := p.peek().Line
	p.advance() // consume 'for'
	varTok, err := p.expect(lexer.IDENT)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.IN); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RANGE); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	limit, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.ForRangeStmt{Line: line, Var: varTok.Literal, Limit: limit, Body: body}, nil
}

func (p *Parser) parsePrintStmt() (*ast.PrintStmt, error) {
	line := p.peek().Line
	p.advance() // consume 'print'
	if _, err := p.expect(lexer.LPAREN); err != nil {
		return nil, err
	}
	val, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RPAREN); err != nil {
		return nil, err
	}
	if err := p.expectNewlineOrEOF(); err != nil {
		return nil, err
	}
	return &ast.PrintStmt{Line: line, Value: val}, nil
}

func (p *Parser) parseExprStmt() (*ast.ExprStmt, error) {
	t := p.peek()
	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expectNewlineOrEOF(); err != nil {
		return nil, err
	}
	return &ast.ExprStmt{Line: t.Line, Expression: expr}, nil
}

// parseBlock reads { stmts... }
func (p *Parser) parseBlock() ([]ast.Statement, error) {
	p.skipNewlines()
	if _, err := p.expect(lexer.LBRACE); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var stmts []ast.Statement
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		p.skipNewlines()
		if p.check(lexer.RBRACE) {
			break
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
		p.skipNewlines()
	}
	if _, err := p.expect(lexer.RBRACE); err != nil {
		return nil, err
	}
	return stmts, nil
}

// ─── expressions (Pratt / precedence climbing) ───────────────────────────────

// Precedence levels (higher = tighter binding)
func infixPrecedence(tt lexer.TokenType) int {
	switch tt {
	case lexer.OR:
		return 1
	case lexer.AND:
		return 2
	case lexer.EQ, lexer.NEQ:
		return 3
	case lexer.LT, lexer.GT, lexer.LTE, lexer.GTE:
		return 4
	case lexer.PLUS, lexer.MINUS:
		return 5
	case lexer.STAR, lexer.SLASH, lexer.PERCENT:
		return 6
	}
	return -1
}

func (p *Parser) parseExpr(minPrec int) (ast.Expression, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for {
		tt := p.peek().Type
		prec := infixPrecedence(tt)
		if prec < minPrec || prec == -1 {
			break
		}
		opTok := p.advance()
		right, err := p.parseExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{Line: opTok.Line, Op: opTok.Literal, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parsePrimary() (ast.Expression, error) {
	t := p.peek()

	switch t.Type {
	case lexer.INT:
		p.advance()
		v, err := strconv.ParseInt(t.Literal, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid integer %q", t.Line, t.Literal)
		}
		return &ast.NumberLit{Line: t.Line, IsFloat: false, IntVal: v}, nil

	case lexer.FLOAT:
		p.advance()
		v, err := strconv.ParseFloat(t.Literal, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid float %q", t.Line, t.Literal)
		}
		return &ast.NumberLit{Line: t.Line, IsFloat: true, FloatVal: v}, nil

	case lexer.STRING:
		p.advance()
		return &ast.StringLit{Line: t.Line, Value: t.Literal}, nil

	case lexer.TRUE:
		p.advance()
		return &ast.BoolLit{Line: t.Line, Value: true}, nil

	case lexer.FALSE:
		p.advance()
		return &ast.BoolLit{Line: t.Line, Value: false}, nil

	case lexer.IDENT:
		p.advance()
		return &ast.IdentExpr{Line: t.Line, Name: t.Literal}, nil

	case lexer.BANG, lexer.MINUS:
		p.advance()
		operand, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Line: t.Line, Op: t.Literal, Operand: operand}, nil

	case lexer.LPAREN:
		p.advance()
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RPAREN); err != nil {
			return nil, err
		}
		return expr, nil
	}

	return nil, fmt.Errorf("line %d: unexpected token %s (%q) in expression", t.Line, t.Type, t.Literal)
}
