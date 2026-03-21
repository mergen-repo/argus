package dsl

import "fmt"

type TokenType int

const (
	TokenIllegal TokenType = iota
	TokenEOF
	TokenComment

	TokenPolicy
	TokenMatch
	TokenRules
	TokenWhen
	TokenAction
	TokenCharging
	TokenIn
	TokenBetween
	TokenAnd
	TokenOr
	TokenNot

	TokenIdent
	TokenString
	TokenNumber

	TokenLParen
	TokenRParen
	TokenLBrace
	TokenRBrace
	TokenComma
	TokenEq
	TokenNeq
	TokenGt
	TokenGte
	TokenLt
	TokenLte
	TokenPercent
	TokenColon
	TokenDash
)

var tokenNames = map[TokenType]string{
	TokenIllegal:  "ILLEGAL",
	TokenEOF:      "EOF",
	TokenComment:  "COMMENT",
	TokenPolicy:   "POLICY",
	TokenMatch:    "MATCH",
	TokenRules:    "RULES",
	TokenWhen:     "WHEN",
	TokenAction:   "ACTION",
	TokenCharging: "CHARGING",
	TokenIn:       "IN",
	TokenBetween:  "BETWEEN",
	TokenAnd:      "AND",
	TokenOr:       "OR",
	TokenNot:      "NOT",
	TokenIdent:    "IDENT",
	TokenString:   "STRING",
	TokenNumber:   "NUMBER",
	TokenLParen:   "(",
	TokenRParen:   ")",
	TokenLBrace:   "{",
	TokenRBrace:   "}",
	TokenComma:    ",",
	TokenEq:       "=",
	TokenNeq:      "!=",
	TokenGt:       ">",
	TokenGte:      ">=",
	TokenLt:       "<",
	TokenLte:      "<=",
	TokenPercent:  "%",
	TokenColon:    ":",
	TokenDash:     "-",
}

var keywords = map[string]TokenType{
	"POLICY":   TokenPolicy,
	"MATCH":    TokenMatch,
	"RULES":    TokenRules,
	"WHEN":     TokenWhen,
	"ACTION":   TokenAction,
	"CHARGING": TokenCharging,
	"IN":       TokenIn,
	"BETWEEN":  TokenBetween,
	"AND":      TokenAnd,
	"OR":       TokenOr,
	"NOT":      TokenNot,
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("TokenType(%d)", int(t))
}

func LookupKeyword(ident string) (TokenType, bool) {
	if tok, ok := keywords[ident]; ok {
		return tok, true
	}
	return TokenIdent, false
}

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q) at %d:%d", t.Type, t.Literal, t.Line, t.Column)
}
