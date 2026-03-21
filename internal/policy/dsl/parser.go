package dsl

import (
	"fmt"
	"strconv"
	"strings"
)

type DSLError struct {
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Snippet  string `json:"snippet,omitempty"`
}

func (e DSLError) Error() string {
	return fmt.Sprintf("%d:%d: %s: %s", e.Line, e.Column, e.Code, e.Message)
}

var unitSet = map[string]bool{
	"bps": true, "kbps": true, "mbps": true, "gbps": true,
	"b": true, "kb": true, "mb": true, "gb": true, "tb": true,
	"s": true, "ms": true, "min": true, "h": true, "d": true,
}

var validRATTypes = map[string]bool{
	"nb_iot": true, "lte_m": true, "lte": true, "nr_5g": true,
}

var validMatchFields = map[string]bool{
	"apn": true, "operator": true, "rat_type": true,
	"sim_type": true, "roaming": true,
}

var validChargingModels = map[string]bool{
	"prepaid": true, "postpaid": true, "hybrid": true,
}

var validOverageActions = map[string]bool{
	"throttle": true, "block": true, "charge": true,
}

var validBillingCycles = map[string]bool{
	"hourly": true, "daily": true, "monthly": true,
}

type Parser struct {
	tokens      []Token
	pos         int
	errors      []DSLError
	sourceLines []string
}

func NewParser(tokens []Token) *Parser {
	return &Parser{
		tokens: tokens,
		pos:    0,
	}
}

func NewParserWithSource(tokens []Token, source string) *Parser {
	return &Parser{
		tokens:      tokens,
		pos:         0,
		sourceLines: strings.Split(source, "\n"),
	}
}

func (p *Parser) Parse() (*Policy, []DSLError) {
	policy := p.parsePolicy()
	return policy, p.errors
}

func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF, Line: 0, Column: 0}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peek() Token {
	next := p.pos + 1
	if next >= len(p.tokens) {
		return Token{Type: TokenEOF, Line: 0, Column: 0}
	}
	return p.tokens[next]
}

func (p *Parser) advance() Token {
	tok := p.current()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *Parser) expect(t TokenType) (Token, bool) {
	tok := p.current()
	if tok.Type != t {
		p.addError(tok.Line, tok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected %s, got %s", t, tok.Type))
		return tok, false
	}
	return p.advance(), true
}

func (p *Parser) addError(line, column int, code, message string) {
	e := DSLError{
		Line:     line,
		Column:   column,
		Severity: "error",
		Code:     code,
		Message:  message,
	}
	if len(p.sourceLines) > 0 && line > 0 && line <= len(p.sourceLines) {
		srcLine := p.sourceLines[line-1]
		pointer := ""
		if column > 0 {
			pointer = strings.Repeat(" ", column-1) + "^"
		}
		e.Snippet = srcLine + "\n" + pointer
	}
	p.errors = append(p.errors, e)
}

func (p *Parser) addWarning(line, column int, code, message string) {
	e := DSLError{
		Line:     line,
		Column:   column,
		Severity: "warning",
		Code:     code,
		Message:  message,
	}
	p.errors = append(p.errors, e)
}

func (p *Parser) skipToRecovery() {
	depth := 0
	for p.current().Type != TokenEOF {
		switch p.current().Type {
		case TokenLBrace:
			depth++
		case TokenRBrace:
			if depth <= 0 {
				return
			}
			depth--
			if depth == 0 {
				p.advance()
				return
			}
		case TokenPolicy, TokenMatch, TokenRules, TokenWhen, TokenCharging:
			if depth == 0 {
				return
			}
		}
		p.advance()
	}
}

func (p *Parser) parsePolicy() *Policy {
	policy := &Policy{}

	if _, ok := p.expect(TokenPolicy); !ok {
		p.skipToRecovery()
		return policy
	}

	if tok, ok := p.expect(TokenString); ok {
		policy.Name = tok.Literal
	} else {
		p.skipToRecovery()
		return policy
	}

	if _, ok := p.expect(TokenLBrace); !ok {
		p.skipToRecovery()
		return policy
	}

	if p.current().Type == TokenMatch {
		policy.Match = p.parseMatchBlock()
	} else {
		p.addError(p.current().Line, p.current().Column, "DSL_MISSING_BLOCK",
			"MATCH block is required")
	}

	if p.current().Type == TokenRules {
		policy.Rules = p.parseRulesBlock()
	} else {
		p.addError(p.current().Line, p.current().Column, "DSL_MISSING_BLOCK",
			"RULES block is required")
	}

	if p.current().Type == TokenCharging {
		policy.Charging = p.parseChargingBlock()
	}

	p.expect(TokenRBrace)

	return policy
}

func (p *Parser) parseMatchBlock() *MatchBlock {
	block := &MatchBlock{}
	p.advance()

	if _, ok := p.expect(TokenLBrace); !ok {
		p.skipToRecovery()
		return block
	}

	for p.current().Type != TokenRBrace && p.current().Type != TokenEOF {
		clause := p.parseMatchClause()
		if clause != nil {
			block.Clauses = append(block.Clauses, clause)
		}
	}

	if len(block.Clauses) == 0 {
		p.addError(p.current().Line, p.current().Column, "DSL_EMPTY_MATCH",
			"MATCH block must have at least one clause")
	}

	p.expect(TokenRBrace)
	return block
}

func (p *Parser) parseMatchClause() *MatchClause {
	clause := &MatchClause{
		Line:   p.current().Line,
		Column: p.current().Column,
	}

	fieldTok := p.current()
	if fieldTok.Type != TokenIdent {
		p.addError(fieldTok.Line, fieldTok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected identifier in MATCH clause, got %s", fieldTok.Type))
		p.skipToRecovery()
		return nil
	}
	clause.Field = fieldTok.Literal
	p.advance()

	if !validMatchFields[clause.Field] && !strings.HasPrefix(clause.Field, "metadata.") {
		p.addWarning(fieldTok.Line, fieldTok.Column, "DSL_UNKNOWN_FIELD",
			fmt.Sprintf("unknown MATCH field %q", clause.Field))
	}

	opTok := p.current()
	switch opTok.Type {
	case TokenIn:
		clause.Operator = "IN"
		p.advance()
	case TokenEq:
		clause.Operator = "="
		p.advance()
	case TokenNeq:
		clause.Operator = "!="
		p.advance()
	default:
		p.addError(opTok.Line, opTok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected operator in MATCH clause, got %s", opTok.Type))
		p.skipToRecovery()
		return nil
	}

	clause.Values = p.parseValueList()
	return clause
}

func (p *Parser) parseRulesBlock() *RulesBlock {
	block := &RulesBlock{}
	p.advance()

	if _, ok := p.expect(TokenLBrace); !ok {
		p.skipToRecovery()
		return block
	}

	seen := make(map[string]int)

	for p.current().Type != TokenRBrace && p.current().Type != TokenEOF {
		stmt := p.parseStatement()
		if stmt == nil {
			continue
		}
		if a, ok := stmt.(*Assignment); ok {
			if prevLine, exists := seen[a.Property]; exists {
				p.addError(a.Line, a.Column, "DSL_DUPLICATE_ASSIGNMENT",
					fmt.Sprintf("duplicate assignment of %q (first at line %d)", a.Property, prevLine))
			}
			seen[a.Property] = a.Line
		}
		block.Statements = append(block.Statements, stmt)
	}

	p.expect(TokenRBrace)
	return block
}

func (p *Parser) parseStatement() Statement {
	tok := p.current()
	switch tok.Type {
	case TokenWhen:
		return p.parseWhenBlock()
	case TokenIdent:
		if p.peek().Type == TokenEq {
			return p.parseAssignment()
		}
		p.addError(tok.Line, tok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("unexpected identifier %q in RULES block", tok.Literal))
		p.advance()
		return nil
	default:
		p.addError(tok.Line, tok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected WHEN or assignment in RULES block, got %s", tok.Type))
		p.advance()
		return nil
	}
}

func (p *Parser) parseAssignment() *Assignment {
	a := &Assignment{
		Line:   p.current().Line,
		Column: p.current().Column,
	}
	identTok := p.advance()
	a.Property = identTok.Literal

	p.advance() // skip '='

	a.Val = p.parseValue()
	return a
}

func (p *Parser) parseWhenBlock() *WhenBlock {
	wb := &WhenBlock{
		Line: p.current().Line,
	}
	p.advance()

	wb.Cond = p.parseConditionExpr()

	if _, ok := p.expect(TokenLBrace); !ok {
		p.skipToRecovery()
		return wb
	}

	seen := make(map[string]int)

	for p.current().Type != TokenRBrace && p.current().Type != TokenEOF {
		body := p.parseWhenBody()
		if body == nil {
			continue
		}
		if a, ok := body.(*Assignment); ok {
			if prevLine, exists := seen[a.Property]; exists {
				p.addError(a.Line, a.Column, "DSL_DUPLICATE_ASSIGNMENT",
					fmt.Sprintf("duplicate assignment of %q within WHEN block (first at line %d)", a.Property, prevLine))
			}
			seen[a.Property] = a.Line
		}
		wb.Body = append(wb.Body, body)
	}

	p.expect(TokenRBrace)
	return wb
}

func (p *Parser) parseWhenBody() WhenBody {
	tok := p.current()
	switch tok.Type {
	case TokenAction:
		return p.parseActionCall()
	case TokenIdent:
		if p.peek().Type == TokenEq {
			return p.parseAssignment()
		}
		p.addError(tok.Line, tok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("unexpected identifier %q in WHEN body", tok.Literal))
		p.advance()
		return nil
	default:
		p.addError(tok.Line, tok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected ACTION or assignment in WHEN body, got %s", tok.Type))
		p.advance()
		return nil
	}
}

func (p *Parser) parseActionCall() *ActionCall {
	ac := &ActionCall{
		Line: p.current().Line,
	}
	p.advance()

	nameTok := p.current()
	if nameTok.Type != TokenIdent {
		p.addError(nameTok.Line, nameTok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected action name after ACTION, got %s", nameTok.Type))
		return ac
	}
	ac.Name = nameTok.Literal
	p.advance()

	if _, ok := p.expect(TokenLParen); !ok {
		return ac
	}

	if p.current().Type != TokenRParen {
		for {
			arg := p.parseArgument()
			if arg != nil {
				ac.Args = append(ac.Args, arg)
			}
			if p.current().Type != TokenComma {
				break
			}
			p.advance()
		}
	}

	p.expect(TokenRParen)

	p.validateAction(ac)

	return ac
}

func (p *Parser) validateAction(ac *ActionCall) {
	switch ac.Name {
	case "notify":
		if len(ac.Args) != 2 {
			p.addError(ac.Line, 0, "DSL_ACTION_PARAMS",
				fmt.Sprintf("notify() requires 2 arguments (event_type, threshold), got %d", len(ac.Args)))
		}
	case "throttle":
		if len(ac.Args) != 1 {
			p.addError(ac.Line, 0, "DSL_ACTION_PARAMS",
				fmt.Sprintf("throttle() requires 1 argument (rate), got %d", len(ac.Args)))
		}
	case "disconnect", "block", "suspend":
		if len(ac.Args) != 0 {
			p.addError(ac.Line, 0, "DSL_ACTION_PARAMS",
				fmt.Sprintf("%s() takes no arguments, got %d", ac.Name, len(ac.Args)))
		}
	case "log":
		if len(ac.Args) != 1 {
			p.addError(ac.Line, 0, "DSL_ACTION_PARAMS",
				fmt.Sprintf("log() requires 1 argument (message), got %d", len(ac.Args)))
		}
	case "tag":
		if len(ac.Args) != 2 {
			p.addError(ac.Line, 0, "DSL_ACTION_PARAMS",
				fmt.Sprintf("tag() requires 2 arguments (key, value), got %d", len(ac.Args)))
		}
	default:
		p.addError(ac.Line, 0, "DSL_UNKNOWN_ACTION",
			fmt.Sprintf("unknown action %q", ac.Name))
	}
}

func (p *Parser) parseArgument() *Argument {
	arg := &Argument{}

	if p.current().Type == TokenIdent && p.peek().Type == TokenEq {
		arg.Name = p.current().Literal
		p.advance()
		p.advance()
	}

	arg.Val = p.parseValue()
	return arg
}

func (p *Parser) parseConditionExpr() Condition {
	return p.parseOrExpr()
}

func (p *Parser) parseOrExpr() Condition {
	left := p.parseAndExpr()
	for p.current().Type == TokenOr {
		p.advance()
		right := p.parseAndExpr()
		left = &CompoundCondition{Left: left, Op: "OR", Right: right}
	}
	return left
}

func (p *Parser) parseAndExpr() Condition {
	left := p.parseUnaryExpr()
	for p.current().Type == TokenAnd {
		p.advance()
		right := p.parseUnaryExpr()
		left = &CompoundCondition{Left: left, Op: "AND", Right: right}
	}
	return left
}

func (p *Parser) parseUnaryExpr() Condition {
	if p.current().Type == TokenNot {
		p.advance()
		inner := p.parseUnaryExpr()
		return &NotCondition{Inner: inner}
	}
	return p.parsePrimaryCondition()
}

func (p *Parser) parsePrimaryCondition() Condition {
	if p.current().Type == TokenLParen {
		p.advance()
		inner := p.parseConditionExpr()
		p.expect(TokenRParen)
		return &GroupCondition{Inner: inner}
	}
	return p.parseSimpleCondition()
}

func (p *Parser) parseSimpleCondition() Condition {
	cond := &SimpleCondition{
		Line:   p.current().Line,
		Column: p.current().Column,
	}

	if p.current().Type != TokenIdent {
		p.addError(p.current().Line, p.current().Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected identifier in condition, got %s", p.current().Type))
		p.advance()
		return cond
	}
	cond.Field = p.current().Literal
	p.advance()

	opTok := p.current()
	switch opTok.Type {
	case TokenIn:
		cond.Operator = "IN"
	case TokenEq:
		cond.Operator = "="
	case TokenNeq:
		cond.Operator = "!="
	case TokenGt:
		cond.Operator = ">"
	case TokenGte:
		cond.Operator = ">="
	case TokenLt:
		cond.Operator = "<"
	case TokenLte:
		cond.Operator = "<="
	case TokenBetween:
		cond.Operator = "BETWEEN"
	default:
		p.addError(opTok.Line, opTok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected operator in condition, got %s", opTok.Type))
		return cond
	}
	p.advance()

	cond.Values = p.parseValueList()
	return cond
}

func (p *Parser) parseValueList() []Value {
	if p.current().Type == TokenLParen {
		p.advance()
		var values []Value
		for p.current().Type != TokenRParen && p.current().Type != TokenEOF {
			v := p.parseValue()
			if v != nil {
				values = append(values, v)
			}
			if p.current().Type == TokenComma {
				p.advance()
			}
		}
		p.expect(TokenRParen)
		return values
	}

	v := p.parseValue()
	if v != nil {
		return []Value{v}
	}
	return nil
}

func (p *Parser) parseValue() Value {
	tok := p.current()

	switch tok.Type {
	case TokenString:
		p.advance()
		return &StringValue{Val: tok.Literal, Line: tok.Line, Column: tok.Column}

	case TokenNumber:
		p.advance()
		num, err := strconv.ParseFloat(tok.Literal, 64)
		if err != nil {
			p.addError(tok.Line, tok.Column, "DSL_INVALID_NUMBER",
				fmt.Sprintf("invalid number: %s", tok.Literal))
			return &NumberValue{Val: 0, Line: tok.Line, Column: tok.Column}
		}

		if p.current().Type == TokenPercent {
			p.advance()
			return &PercentValue{Val: num, Line: tok.Line, Column: tok.Column}
		}

		if p.current().Type == TokenColon && len(tok.Literal) == 2 {
			return p.parseTimeRangeFrom(tok, num)
		}

		if p.current().Type == TokenIdent {
			unitTok := p.current()
			unitLower := strings.ToLower(unitTok.Literal)
			if unitSet[unitLower] {
				p.advance()
				return &NumberWithUnit{Val: num, Unit: unitLower, Line: tok.Line, Column: tok.Column}
			}
		}

		return &NumberValue{Val: num, Line: tok.Line, Column: tok.Column}

	case TokenIdent:
		p.advance()
		if tok.Literal == "true" {
			return &BoolValue{Val: true, Line: tok.Line, Column: tok.Column}
		}
		if tok.Literal == "false" {
			return &BoolValue{Val: false, Line: tok.Line, Column: tok.Column}
		}

		if p.current().Type == TokenColon {
			return p.parseTimeRangeFromIdent(tok)
		}

		return &IdentValue{Val: tok.Literal, Line: tok.Line, Column: tok.Column}

	default:
		p.addError(tok.Line, tok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("expected value, got %s", tok.Type))
		p.advance()
		return nil
	}
}

func (p *Parser) parseTimeRangeFrom(hourTok Token, hourNum float64) Value {
	p.advance() // consume ':'
	minTok := p.current()
	if minTok.Type != TokenNumber {
		p.addError(minTok.Line, minTok.Column, "DSL_SYNTAX_ERROR",
			"expected minutes in time")
		return &NumberValue{Val: hourNum, Line: hourTok.Line, Column: hourTok.Column}
	}
	p.advance()

	startTime := fmt.Sprintf("%s:%s", hourTok.Literal, minTok.Literal)

	if p.current().Type != TokenDash {
		return &StringValue{Val: startTime, Line: hourTok.Line, Column: hourTok.Column}
	}
	p.advance()

	endHour := p.current()
	if endHour.Type != TokenNumber {
		p.addError(endHour.Line, endHour.Column, "DSL_SYNTAX_ERROR",
			"expected hour in time range end")
		return &StringValue{Val: startTime, Line: hourTok.Line, Column: hourTok.Column}
	}
	p.advance()

	if _, ok := p.expect(TokenColon); !ok {
		return &StringValue{Val: startTime, Line: hourTok.Line, Column: hourTok.Column}
	}

	endMin := p.current()
	if endMin.Type != TokenNumber {
		p.addError(endMin.Line, endMin.Column, "DSL_SYNTAX_ERROR",
			"expected minutes in time range end")
		return &StringValue{Val: startTime, Line: hourTok.Line, Column: hourTok.Column}
	}
	p.advance()

	endTime := fmt.Sprintf("%s:%s", endHour.Literal, endMin.Literal)
	return &TimeRange{Start: startTime, End: endTime, Line: hourTok.Line, Column: hourTok.Column}
}

func (p *Parser) parseTimeRangeFromIdent(tok Token) Value {
	if len(tok.Literal) < 4 {
		return &IdentValue{Val: tok.Literal, Line: tok.Line, Column: tok.Column}
	}

	return &IdentValue{Val: tok.Literal, Line: tok.Line, Column: tok.Column}
}

func (p *Parser) parseChargingBlock() *ChargingBlock {
	block := &ChargingBlock{
		RATMultiplier: make(map[string]float64),
	}
	p.advance()

	if _, ok := p.expect(TokenLBrace); !ok {
		p.skipToRecovery()
		return block
	}

	seen := make(map[string]int)

	for p.current().Type != TokenRBrace && p.current().Type != TokenEOF {
		tok := p.current()
		if tok.Type == TokenIdent && tok.Literal == "rat_type_multiplier" {
			p.advance()
			if _, ok := p.expect(TokenLBrace); !ok {
				p.skipToRecovery()
				continue
			}
			for p.current().Type != TokenRBrace && p.current().Type != TokenEOF {
				ratTok := p.current()
				if ratTok.Type != TokenIdent {
					p.addError(ratTok.Line, ratTok.Column, "DSL_SYNTAX_ERROR",
						"expected RAT type identifier")
					p.advance()
					continue
				}
				ratName := ratTok.Literal
				if !validRATTypes[ratName] {
					p.addError(ratTok.Line, ratTok.Column, "DSL_INVALID_RAT_TYPE",
						fmt.Sprintf("invalid RAT type %q, must be one of: nb_iot, lte_m, lte, nr_5g", ratName))
				}
				p.advance()
				p.expect(TokenEq)
				valTok := p.current()
				if valTok.Type != TokenNumber {
					p.addError(valTok.Line, valTok.Column, "DSL_SYNTAX_ERROR",
						"expected number for RAT type multiplier")
					p.advance()
					continue
				}
				val, _ := strconv.ParseFloat(valTok.Literal, 64)
				block.RATMultiplier[ratName] = val
				p.advance()
			}
			p.expect(TokenRBrace)
			continue
		}

		if tok.Type == TokenIdent && p.peek().Type == TokenEq {
			a := p.parseAssignment()
			if a != nil {
				if prevLine, exists := seen[a.Property]; exists {
					p.addError(a.Line, a.Column, "DSL_DUPLICATE_ASSIGNMENT",
						fmt.Sprintf("duplicate assignment of %q in CHARGING block (first at line %d)", a.Property, prevLine))
				}
				seen[a.Property] = a.Line
				block.Assignments = append(block.Assignments, a)
			}
			continue
		}

		p.addError(tok.Line, tok.Column, "DSL_SYNTAX_ERROR",
			fmt.Sprintf("unexpected token %s in CHARGING block", tok.Type))
		p.advance()
	}

	p.expect(TokenRBrace)
	return block
}
