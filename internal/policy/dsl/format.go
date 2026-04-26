package dsl

import (
	"strings"
)

// Format takes raw DSL source and returns a normalized version with
// consistent 2-space indentation, one statement per line inside blocks,
// canonical whitespace around operators, stripped trailing whitespace,
// and a single trailing newline.
//
// Format is conservative:
//   - Comments (# ...) are preserved verbatim on their own lines, indented
//     to the surrounding block depth.
//   - String literals are preserved verbatim (including their quotes and
//     internal whitespace).
//   - On parse failure (any DSLError of severity "error") the original
//     source is returned unchanged — formatting an unparseable source can
//     mangle it further. The returned error is always nil in this case;
//     callers that need to know "did anything change" can compare strings.
//
// Format is idempotent: Format(Format(s)) == Format(s).
//
// Implementation note: this is a token-based reformatter built on the
// existing Lexer (token.go). It does NOT use the AST, because the AST
// drops comments — and we want to preserve them. The lexer's TokenComment
// path is a no-op (skipComment consumes the comment without emitting a
// token), so we additionally scan the raw source for comment lines and
// re-inject them at their original line position.
//
// FIX-243 Wave D — AC-8.
func Format(source string) (string, error) {
	if errs := Validate(source); hasError(errs) {
		// Don't attempt to reformat unparseable input — risk of mangling.
		return source, nil
	}
	return formatTokens(source), nil
}

func hasError(errs []DSLError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}

// formatTokens re-emits the lexed token stream with canonical whitespace.
// Original comment lines are interleaved by line number so that authoring
// intent (a comment above a rule) is preserved.
func formatTokens(source string) string {
	tokens := NewLexer(source).Tokenize()
	commentsByLine := extractComments(source)

	var sb strings.Builder
	indent := 0
	const step = "  " // 2 spaces

	emitIndent := func() {
		for i := 0; i < indent; i++ {
			sb.WriteString(step)
		}
	}

	// Track which comment lines we've already flushed, so a comment that
	// appears on the same source line as a token isn't emitted twice.
	flushedComments := make(map[int]bool, len(commentsByLine))

	// Helper: flush any comment lines whose source line is < tok.Line.
	flushCommentsBefore := func(line int) {
		for ln := 1; ln < line; ln++ {
			if c, ok := commentsByLine[ln]; ok && !flushedComments[ln] {
				emitIndent()
				sb.WriteString(c)
				sb.WriteByte('\n')
				flushedComments[ln] = true
			}
		}
	}

	// Token-level state machine. We treat the token stream as a sequence
	// of "statements" terminated by '{', '}', or by a line break in the
	// original source between two non-bracket tokens (since the DSL has
	// no explicit terminator).
	type tokState struct {
		lastLine    int
		lastCol     int
		lastLen     int
		lastType    TokenType
		atLineStart bool // true when the next emitted token should start on a fresh line
		needSpace   bool // true when the next token needs a leading space
	}
	st := tokState{lastLine: 0, atLineStart: true, needSpace: false}

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.Type == TokenEOF {
			break
		}

		flushCommentsBefore(tok.Line)

		switch tok.Type {
		case TokenLBrace:
			// Opening brace: stays on same line as the previous header
			// (POLICY "x" {  /  MATCH {  /  WHEN ... {).
			if !st.atLineStart {
				sb.WriteByte(' ')
			}
			sb.WriteByte('{')
			sb.WriteByte('\n')
			indent++
			st.atLineStart = true
			st.needSpace = false
		case TokenRBrace:
			// Closing brace: on its own line, dedented.
			if !st.atLineStart {
				sb.WriteByte('\n')
			}
			if indent > 0 {
				indent--
			}
			emitIndent()
			sb.WriteByte('}')
			sb.WriteByte('\n')
			st.atLineStart = true
			st.needSpace = false
		default:
			// New statement boundary if the source advanced to a new line
			// between the previous emitted token and this one.
			if st.lastLine != 0 && tok.Line != st.lastLine && !st.atLineStart {
				sb.WriteByte('\n')
				st.atLineStart = true
				st.needSpace = false
			}

			// Detect "unit suffix" pattern: an IDENT immediately following
			// a NUMBER on the same source line with no whitespace between
			// them (e.g. "10mbps", "24h", "500MB"). Preserve attachment.
			attachToPrev := false
			if tok.Type == TokenIdent && st.lastType == TokenNumber &&
				tok.Line == st.lastLine && tok.Column == st.lastCol+st.lastLen {
				attachToPrev = true
			}

			if st.atLineStart {
				emitIndent()
				st.atLineStart = false
				st.needSpace = false
			} else if attachToPrev {
				// no separator
			} else if tokenNeedsLeadingSpace(tok.Type) && st.needSpace {
				sb.WriteByte(' ')
			}

			sb.WriteString(renderToken(tok))
			st.needSpace = tokenNeedsTrailingSpace(tok.Type)
		}

		st.lastLine = tok.Line
		st.lastCol = tok.Column
		st.lastLen = len(tok.Literal)
		st.lastType = tok.Type
	}

	// Flush any trailing comments that appeared after the last token.
	if len(commentsByLine) > 0 {
		maxLine := 0
		for ln := range commentsByLine {
			if ln > maxLine {
				maxLine = ln
			}
		}
		for ln := 1; ln <= maxLine; ln++ {
			if c, ok := commentsByLine[ln]; ok && !flushedComments[ln] {
				emitIndent()
				sb.WriteString(c)
				sb.WriteByte('\n')
				flushedComments[ln] = true
			}
		}
	}

	out := sb.String()
	// Collapse 3+ consecutive newlines to 2 (allow one blank line between
	// blocks for readability, but no more).
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	// Strip trailing whitespace from each line.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	out = strings.Join(lines, "\n")
	// Ensure exactly one final newline.
	out = strings.TrimRight(out, "\n") + "\n"
	return out
}

// renderToken returns the canonical surface form for a token. Identifiers
// stay lowercase (the lexer already lowercases them), keywords stay
// uppercase, strings keep their quotes, numbers keep their literal text.
func renderToken(t Token) string {
	switch t.Type {
	case TokenString:
		return "\"" + t.Literal + "\""
	case TokenPolicy, TokenMatch, TokenRules, TokenWhen, TokenAction,
		TokenCharging, TokenIn, TokenBetween, TokenAnd, TokenOr, TokenNot:
		return t.Literal
	case TokenEq, TokenNeq, TokenGt, TokenGte, TokenLt, TokenLte:
		return t.Literal
	case TokenLParen:
		return "("
	case TokenRParen:
		return ")"
	case TokenComma:
		return ","
	case TokenPercent:
		return "%"
	case TokenColon:
		return ":"
	case TokenDash:
		return "-"
	default:
		return t.Literal
	}
}

// tokenNeedsLeadingSpace reports whether this token should be preceded by
// a space when it follows another token on the same emitted line.
func tokenNeedsLeadingSpace(t TokenType) bool {
	switch t {
	case TokenRParen, TokenComma, TokenPercent, TokenColon, TokenDash:
		return false
	}
	return true
}

// tokenNeedsTrailingSpace reports whether this token expects the next
// token (if any, on the same line) to be separated by a space.
func tokenNeedsTrailingSpace(t TokenType) bool {
	switch t {
	case TokenLParen, TokenDash, TokenColon:
		return false
	}
	return true
}

// extractComments returns a map of line-number → comment text (including
// the leading '#'). The DSL lexer skips comments entirely; we re-scan the
// raw source so we can re-emit them at their original location.
func extractComments(source string) map[int]string {
	out := map[int]string{}
	lines := strings.Split(source, "\n")
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "#") {
			out[i+1] = trimmed
		}
	}
	return out
}
