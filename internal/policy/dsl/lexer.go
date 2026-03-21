package dsl

import (
	"strings"
	"unicode"
)

type Lexer struct {
	input   string
	pos     int
	readPos int
	ch      byte
	line    int
	column  int
}

func NewLexer(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 0,
	}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++
	l.column++
}

func (l *Lexer) peekChar() byte {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

func (l *Lexer) skipWhitespace() {
	for l.ch != 0 && (l.ch == ' ' || l.ch == '\t' || l.ch == '\r' || l.ch == '\n') {
		if l.ch == '\n' {
			l.line++
			l.column = 0
		}
		l.readChar()
	}
}

func (l *Lexer) skipComment() {
	for l.ch != 0 && l.ch != '\n' {
		l.readChar()
	}
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.ch == '#' {
		l.skipComment()
		return l.NextToken()
	}

	tok := Token{
		Line:   l.line,
		Column: l.column,
	}

	switch l.ch {
	case 0:
		tok.Type = TokenEOF
		tok.Literal = ""
	case '{':
		tok.Type = TokenLBrace
		tok.Literal = "{"
		l.readChar()
	case '}':
		tok.Type = TokenRBrace
		tok.Literal = "}"
		l.readChar()
	case '(':
		tok.Type = TokenLParen
		tok.Literal = "("
		l.readChar()
	case ')':
		tok.Type = TokenRParen
		tok.Literal = ")"
		l.readChar()
	case ',':
		tok.Type = TokenComma
		tok.Literal = ","
		l.readChar()
	case '%':
		tok.Type = TokenPercent
		tok.Literal = "%"
		l.readChar()
	case ':':
		tok.Type = TokenColon
		tok.Literal = ":"
		l.readChar()
	case '-':
		tok.Type = TokenDash
		tok.Literal = "-"
		l.readChar()
	case '=':
		tok.Type = TokenEq
		tok.Literal = "="
		l.readChar()
	case '!':
		if l.peekChar() == '=' {
			tok.Type = TokenNeq
			tok.Literal = "!="
			l.readChar()
			l.readChar()
		} else {
			tok.Type = TokenIllegal
			tok.Literal = string(l.ch)
			l.readChar()
		}
	case '>':
		if l.peekChar() == '=' {
			tok.Type = TokenGte
			tok.Literal = ">="
			l.readChar()
			l.readChar()
		} else {
			tok.Type = TokenGt
			tok.Literal = ">"
			l.readChar()
		}
	case '<':
		if l.peekChar() == '=' {
			tok.Type = TokenLte
			tok.Literal = "<="
			l.readChar()
			l.readChar()
		} else {
			tok.Type = TokenLt
			tok.Literal = "<"
			l.readChar()
		}
	case '"':
		tok.Type = TokenString
		tok.Literal = l.readString()
	default:
		if isDigit(l.ch) {
			tok.Literal = l.readNumber()
			tok.Type = TokenNumber
			return tok
		}
		if isLetter(l.ch) || l.ch == '_' {
			literal := l.readIdentifier()
			if kwType, ok := LookupKeyword(literal); ok {
				tok.Type = kwType
				tok.Literal = literal
			} else {
				tok.Type = TokenIdent
				tok.Literal = strings.ToLower(literal)
			}
			return tok
		}
		tok.Type = TokenIllegal
		tok.Literal = string(l.ch)
		l.readChar()
	}

	return tok
}

func (l *Lexer) readString() string {
	l.readChar()
	start := l.pos
	var sb strings.Builder
	for l.ch != 0 && l.ch != '"' {
		if l.ch == '\\' && l.peekChar() == '"' {
			sb.WriteString(l.input[start:l.pos])
			l.readChar()
			sb.WriteByte('"')
			l.readChar()
			start = l.pos
			continue
		}
		if l.ch == '\n' {
			l.line++
			l.column = 0
		}
		l.readChar()
	}
	sb.WriteString(l.input[start:l.pos])
	if l.ch == '"' {
		l.readChar()
	}
	return sb.String()
}

func (l *Lexer) readNumber() string {
	start := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' || l.ch == '.' || l.ch == '*' {
		l.readChar()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) Tokenize() []Token {
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}
	return tokens
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isLetter(ch byte) bool {
	return unicode.IsLetter(rune(ch))
}
