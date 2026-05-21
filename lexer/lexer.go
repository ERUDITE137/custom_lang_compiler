package lexer

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TokenType identifies what kind of token this is.
type TokenType string

const (
	// Literals
	INT    TokenType = "INT"
	FLOAT  TokenType = "FLOAT"
	STRING TokenType = "STRING"
	TRUE   TokenType = "TRUE"
	FALSE  TokenType = "FALSE"

	// Identifiers & keywords
	IDENT     TokenType = "IDENT"
	LET       TokenType = "LET"
	IF        TokenType = "IF"
	ELSE      TokenType = "ELSE"
	FOR       TokenType = "FOR"
	IN        TokenType = "IN"
	RANGE     TokenType = "RANGE"
	PRINT     TokenType = "PRINT"
	WHILE  TokenType = "WHILE"
	SPAWN  TokenType = "SPAWN"
	JOIN   TokenType = "JOIN"
	ATOMIC TokenType = "ATOMIC"
	RETRY  TokenType = "RETRY"

	// Operators
	PLUS     TokenType = "+"
	MINUS    TokenType = "-"
	STAR     TokenType = "*"
	SLASH    TokenType = "/"
	PERCENT  TokenType = "%"
	BANG     TokenType = "!"
	AND      TokenType = "&&"
	OR       TokenType = "||"
	EQ       TokenType = "=="
	NEQ      TokenType = "!="
	LT       TokenType = "<"
	GT       TokenType = ">"
	LTE      TokenType = "<="
	GTE      TokenType = ">="
	ASSIGN   TokenType = "="

	// Delimiters
	LPAREN   TokenType = "("
	RPAREN   TokenType = ")"
	LBRACE   TokenType = "{"
	RBRACE   TokenType = "}"
	COMMA    TokenType = ","
	NEWLINE  TokenType = "NEWLINE"

	// Special
	EOF     TokenType = "EOF"
	ILLEGAL TokenType = "ILLEGAL"
)

var keywords = map[string]TokenType{
	"let":       LET,
	"if":        IF,
	"else":      ELSE,
	"for":       FOR,
	"in":        IN,
	"range":     RANGE,
	"print":     PRINT,
	"true":      TRUE,
	"false":     FALSE,
	"while":  WHILE,
	"spawn":  SPAWN,
	"join":   JOIN,
	"atomic": ATOMIC,
	"retry":  RETRY,
}

// Token is a single lexical unit.
type Token struct {
	Type    TokenType
	Literal string
	Line    int
}

func (t Token) String() string {
	return fmt.Sprintf("Token(%s, %q, line %d)", t.Type, t.Literal, t.Line)
}

// Lexer holds the state of the scanner.
type Lexer struct {
	src    string
	pos    int // current byte offset
	line   int
	tokens []Token
}

// New creates a Lexer and tokenizes the full source.
func New(src string) (*Lexer, error) {
	l := &Lexer{src: src, line: 1}
	if err := l.tokenize(); err != nil {
		return nil, err
	}
	return l, nil
}

// Tokens returns the complete token slice.
func (l *Lexer) Tokens() []Token { return l.tokens }

func (l *Lexer) tokenize() error {
	for l.pos < len(l.src) {
		ch, size := utf8.DecodeRuneInString(l.src[l.pos:])

		switch {
		case ch == '\n':
			// Emit a NEWLINE only if previous token is meaningful
			if len(l.tokens) > 0 {
				prev := l.tokens[len(l.tokens)-1]
				if prev.Type != NEWLINE && prev.Type != LBRACE {
					l.tokens = append(l.tokens, Token{NEWLINE, "\\n", l.line})
				}
			}
			l.line++
			l.pos += size

		case ch == '\r':
			l.pos += size

		case unicode.IsSpace(ch):
			l.pos += size

		case ch == '#':
			// Line comment — skip to end of line
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}

		case ch == '"':
			tok, err := l.readString()
			if err != nil {
				return err
			}
			l.tokens = append(l.tokens, tok)

		case unicode.IsDigit(ch):
			l.tokens = append(l.tokens, l.readNumber())

		case unicode.IsLetter(ch) || ch == '_':
			l.tokens = append(l.tokens, l.readIdent())

		default:
			tok, err := l.readSymbol()
			if err != nil {
				return err
			}
			if tok.Type != ILLEGAL {
				l.tokens = append(l.tokens, tok)
			}
		}
	}
	l.tokens = append(l.tokens, Token{EOF, "", l.line})
	return nil
}

func (l *Lexer) readString() (Token, error) {
	line := l.line
	l.pos++ // skip opening "
	var sb strings.Builder
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == '"' {
			l.pos++
			return Token{STRING, sb.String(), line}, nil
		}
		if ch == '\\' && l.pos+1 < len(l.src) {
			l.pos++
			switch l.src[l.pos] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte('\\')
				sb.WriteByte(l.src[l.pos])
			}
			l.pos++
			continue
		}
		sb.WriteByte(ch)
		l.pos++
	}
	return Token{}, fmt.Errorf("line %d: unterminated string literal", line)
}

func (l *Lexer) readNumber() Token {
	line := l.line
	start := l.pos
	isFloat := false
	for l.pos < len(l.src) && (unicode.IsDigit(rune(l.src[l.pos])) || l.src[l.pos] == '.') {
		if l.src[l.pos] == '.' {
			if isFloat {
				break // second dot: stop
			}
			isFloat = true
		}
		l.pos++
	}
	lit := l.src[start:l.pos]
	if isFloat {
		return Token{FLOAT, lit, line}
	}
	return Token{INT, lit, line}
}

func (l *Lexer) readIdent() Token {
	line := l.line
	start := l.pos
	for l.pos < len(l.src) {
		ch, size := utf8.DecodeRuneInString(l.src[l.pos:])
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' {
			break
		}
		l.pos += size
	}
	lit := l.src[start:l.pos]
	tt, ok := keywords[lit]
	if !ok {
		tt = IDENT
	}
	return Token{tt, lit, line}
}

func (l *Lexer) readSymbol() (Token, error) {
	line := l.line
	ch := l.src[l.pos]
	next := byte(0)
	if l.pos+1 < len(l.src) {
		next = l.src[l.pos+1]
	}

	two := func(tt TokenType, lit string) Token {
		l.pos += 2
		return Token{tt, lit, line}
	}
	one := func(tt TokenType, lit string) Token {
		l.pos++
		return Token{tt, lit, line}
	}

	switch ch {
	case '+':
		return one(PLUS, "+"), nil
	case '-':
		return one(MINUS, "-"), nil
	case '*':
		return one(STAR, "*"), nil
	case '/':
		return one(SLASH, "/"), nil
	case '%':
		return one(PERCENT, "%"), nil
	case '(':
		return one(LPAREN, "("), nil
	case ')':
		return one(RPAREN, ")"), nil
	case '{':
		return one(LBRACE, "{"), nil
	case '}':
		return one(RBRACE, "}"), nil
	case ',':
		return one(COMMA, ","), nil
	case '&':
		if next == '&' {
			return two(AND, "&&"), nil
		}
	case '|':
		if next == '|' {
			return two(OR, "||"), nil
		}
	case '=':
		if next == '=' {
			return two(EQ, "=="), nil
		}
		return one(ASSIGN, "="), nil
	case '!':
		if next == '=' {
			return two(NEQ, "!="), nil
		}
		return one(BANG, "!"), nil
	case '<':
		if next == '=' {
			return two(LTE, "<="), nil
		}
		return one(LT, "<"), nil
	case '>':
		if next == '=' {
			return two(GTE, ">="), nil
		}
		return one(GT, ">"), nil
	}

	l.pos++
	return Token{}, fmt.Errorf("line %d: unexpected character %q", line, ch)
}
