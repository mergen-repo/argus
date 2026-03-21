package dsl

import (
	"testing"
)

func TestLexer_Keywords(t *testing.T) {
	input := "POLICY MATCH RULES WHEN ACTION CHARGING IN BETWEEN AND OR NOT"
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenPolicy, TokenMatch, TokenRules, TokenWhen, TokenAction,
		TokenCharging, TokenIn, TokenBetween, TokenAnd, TokenOr, TokenNot,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d]: expected %s, got %s (%q)", i, exp, tokens[i].Type, tokens[i].Literal)
		}
	}
}

func TestLexer_Identifiers(t *testing.T) {
	input := "bandwidth_down Bandwidth_Up RAT_TYPE metadata.fleet_id"
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()

	expected := []struct {
		typ     TokenType
		literal string
	}{
		{TokenIdent, "bandwidth_down"},
		{TokenIdent, "bandwidth_up"},
		{TokenIdent, "rat_type"},
		{TokenIdent, "metadata.fleet_id"},
		{TokenEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ {
			t.Errorf("token[%d]: expected type %s, got %s", i, exp.typ, tokens[i].Type)
		}
		if tokens[i].Literal != exp.literal {
			t.Errorf("token[%d]: expected literal %q, got %q", i, exp.literal, tokens[i].Literal)
		}
	}
}

func TestLexer_Strings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", `"hello"`, "hello"},
		{"with spaces", `"iot.fleet"`, "iot.fleet"},
		{"escaped quote", `"say \"hi\""`, `say "hi"`},
		{"empty", `""`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tok := lexer.NextToken()
			if tok.Type != TokenString {
				t.Fatalf("expected STRING, got %s", tok.Type)
			}
			if tok.Literal != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tok.Literal)
			}
		})
	}
}

func TestLexer_Numbers(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"42", "42"},
		{"3.14", "3.14"},
		{"0.01", "0.01"},
		{"1000", "1000"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tok := lexer.NextToken()
			if tok.Type != TokenNumber {
				t.Fatalf("expected NUMBER, got %s", tok.Type)
			}
			if tok.Literal != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tok.Literal)
			}
		})
	}
}

func TestLexer_Operators(t *testing.T) {
	input := "= != > >= < <="
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenEq, TokenNeq, TokenGt, TokenGte, TokenLt, TokenLte, TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d]: expected %s, got %s", i, exp, tokens[i].Type)
		}
	}
}

func TestLexer_Structural(t *testing.T) {
	input := `{ } ( ) , % : -`
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenLBrace, TokenRBrace, TokenLParen, TokenRParen,
		TokenComma, TokenPercent, TokenColon, TokenDash, TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d]: expected %s, got %s", i, exp, tokens[i].Type)
		}
	}
}

func TestLexer_SkipsWhitespaceAndComments(t *testing.T) {
	input := `POLICY  "test" # this is a comment
	{
		MATCH # another comment
	}`
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenPolicy, TokenString, TokenLBrace, TokenMatch, TokenRBrace, TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d]: expected %s, got %s", i, exp, tokens[i].Type)
		}
	}
}

func TestLexer_LineColumnTracking(t *testing.T) {
	input := "POLICY \"test\" {\n  MATCH {\n  }\n}"
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()

	if tokens[0].Line != 1 || tokens[0].Column != 1 {
		t.Errorf("POLICY: expected 1:1, got %d:%d", tokens[0].Line, tokens[0].Column)
	}

	matchIdx := -1
	for i, tok := range tokens {
		if tok.Type == TokenMatch {
			matchIdx = i
			break
		}
	}
	if matchIdx >= 0 && tokens[matchIdx].Line != 2 {
		t.Errorf("MATCH: expected line 2, got %d", tokens[matchIdx].Line)
	}
}

func TestLexer_CompletePolicy(t *testing.T) {
	input := `POLICY "iot-fleet-standard" {
    MATCH {
        apn IN ("iot.fleet", "iot.meter")
        rat_type IN (nb_iot, lte_m)
    }

    RULES {
        bandwidth_down = 1mbps
        bandwidth_up = 256kbps

        WHEN usage > 1GB {
            bandwidth_down = 64kbps
            ACTION notify(quota_exceeded, 100%)
        }
    }

    CHARGING {
        model = postpaid
        rate_per_mb = 0.01
    }
}`
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()

	if tokens[0].Type != TokenPolicy {
		t.Errorf("first token should be POLICY, got %s", tokens[0].Type)
	}

	lastReal := tokens[len(tokens)-2]
	if lastReal.Type != TokenRBrace {
		t.Errorf("last real token should be }, got %s", lastReal.Type)
	}

	if tokens[len(tokens)-1].Type != TokenEOF {
		t.Error("last token should be EOF")
	}

	keywordCount := 0
	for _, tok := range tokens {
		switch tok.Type {
		case TokenPolicy, TokenMatch, TokenRules, TokenWhen, TokenAction, TokenCharging, TokenIn:
			keywordCount++
		}
	}
	if keywordCount < 7 {
		t.Errorf("expected at least 7 keywords, got %d", keywordCount)
	}
}
