package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/spf13/cobra"
)

var validateSeedCmd = &cobra.Command{
	Use:   "validate-seed-dsl",
	Short: "Validate DSL strings embedded in migrations/seed/*.sql files",
	Long: `FIX-243 — walks seed SQL files, extracts DSL strings from policy_versions
INSERT statements, validates each via the policy DSL parser/validator.
Exits 1 if any DSL string fails to parse cleanly.

Supports both Postgres dollar-quoted ($tag$ ... $tag$) and standard single-quoted
('...' with '' escape) string forms.`,
	RunE: runValidateSeedDSL,
}

var (
	seedDir    string
	seedStrict bool
)

func init() {
	validateSeedCmd.Flags().StringVar(&seedDir, "seed-dir", "./migrations/seed", "directory containing seed *.sql files")
	validateSeedCmd.Flags().BoolVar(&seedStrict, "strict", false, "fail on warnings as well as errors")
	rootCmd.AddCommand(validateSeedCmd)
}

func runValidateSeedDSL(cmd *cobra.Command, _ []string) error {
	files, err := filepath.Glob(filepath.Join(seedDir, "*.sql"))
	if err != nil {
		return fmt.Errorf("glob %s: %w", seedDir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no .sql files in %s", seedDir)
	}

	out := cmd.OutOrStdout()
	failed := 0
	totalChecked := 0

	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		dslStrings := extractDSLStrings(string(body))
		if len(dslStrings) == 0 {
			fmt.Fprintf(out, "[SKIP] %s — no policy_versions DSL strings\n", filepath.Base(f))
			continue
		}
		for _, s := range dslStrings {
			totalChecked++
			errs := dsl.Validate(s.Source)
			errCount, warnCount := 0, 0
			for _, e := range errs {
				if e.Severity == "error" {
					errCount++
				} else if e.Severity == "warning" {
					warnCount++
				}
			}
			if errCount > 0 || (seedStrict && warnCount > 0) {
				failed++
				fmt.Fprintf(out, "[FAIL] %s line~%d: %d errors, %d warnings\n",
					filepath.Base(f), s.LineApprox, errCount, warnCount)
				for _, e := range errs {
					fmt.Fprintf(out, "  %s L%d:%d %s — %s\n",
						e.Severity, e.Line, e.Column, e.Code, e.Message)
				}
			} else {
				fmt.Fprintf(out, "[OK]   %s line~%d (%d warnings)\n",
					filepath.Base(f), s.LineApprox, warnCount)
			}
		}
	}

	fmt.Fprintf(out, "\nChecked %d DSL strings, %d failure(s)\n", totalChecked, failed)
	if failed > 0 {
		return fmt.Errorf("%d invalid DSL string(s)", failed)
	}
	return nil
}

type extractedDSL struct {
	Source     string
	LineApprox int
}

// extractDSLStrings finds every dsl_content literal embedded in
// `INSERT INTO policy_versions ...` blocks of a SQL seed file. It supports both
// Postgres dollar-quoted ($tag$...$tag$) literals and standard single-quoted
// literals ('...' with '' as escape). The MVP filter retains only candidate
// strings whose trimmed prefix is POLICY or IF (the two DSL forms in use);
// JSON `compiled_rules`, UUIDs, descriptions, hashes, etc. are skipped.
func extractDSLStrings(body string) []extractedDSL {
	out := []extractedDSL{}
	headerRe := regexp.MustCompile(`(?i)INSERT\s+INTO\s+policy_versions\b`)
	headers := headerRe.FindAllStringIndex(body, -1)
	if len(headers) == 0 {
		return out
	}

	for _, hdr := range headers {
		start := hdr[1]
		// Bound the block at the next unquoted `;` — this ends the INSERT
		// statement and prevents downstream INSERT bodies (e.g. notifications
		// titles like 'Policy Activated') from being misclassified.
		end := findStatementEnd(body, start)
		block := body[start:end]
		baseLine := lineOf(body, start)

		// 1) dollar-quoted $tag$...$tag$ — RE2 has no backreferences, so scan manually
		for _, dq := range scanDollarQuoted(block) {
			if isDSLCandidate(dq.lit) {
				out = append(out, extractedDSL{
					Source:     dq.lit,
					LineApprox: baseLine + lineOf(block, dq.start) - 1,
				})
			}
		}

		// 2) single-quoted strings with '' escape
		j := 0
		for j < len(block) {
			c := block[j]
			if c == '\'' {
				strStart := j + 1
				j++
				var sb strings.Builder
				for j < len(block) {
					if block[j] == '\'' {
						if j+1 < len(block) && block[j+1] == '\'' {
							sb.WriteByte('\'')
							j += 2
							continue
						}
						break
					}
					sb.WriteByte(block[j])
					j++
				}
				lit := sb.String()
				if isDSLCandidate(lit) {
					out = append(out, extractedDSL{
						Source:     lit,
						LineApprox: baseLine + lineOf(block, strStart) - 1,
					})
				}
				if j < len(block) {
					j++
				}
				continue
			}
			// Skip line / block comments to avoid `''` inside `--` confusing us
			if c == '-' && j+1 < len(block) && block[j+1] == '-' {
				for j < len(block) && block[j] != '\n' {
					j++
				}
				continue
			}
			j++
		}
	}

	return out
}

// findStatementEnd returns the offset of the SQL statement-terminating `;`
// (or end-of-body) starting from `start`, treating single-quoted strings
// (with '' escape), dollar-quoted strings, and -- line comments as opaque.
func findStatementEnd(body string, start int) int {
	i := start
	for i < len(body) {
		c := body[i]
		switch {
		case c == ';':
			return i
		case c == '\'':
			i++
			for i < len(body) {
				if body[i] == '\'' {
					if i+1 < len(body) && body[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '-' && i+1 < len(body) && body[i+1] == '-':
			for i < len(body) && body[i] != '\n' {
				i++
			}
		case c == '$':
			// Try to skip a dollar-quoted literal starting at i.
			tagEnd := -1
			for k := i + 1; k < len(body); k++ {
				bc := body[k]
				if bc == '$' {
					tagEnd = k
					break
				}
				if !(bc == '_' || (bc >= 'A' && bc <= 'Z') || (bc >= 'a' && bc <= 'z') ||
					(k > i+1 && bc >= '0' && bc <= '9')) {
					break
				}
			}
			if tagEnd > 0 {
				tag := body[i : tagEnd+1]
				close := strings.Index(body[tagEnd+1:], tag)
				if close != -1 {
					i = tagEnd + 1 + close + len(tag)
					continue
				}
			}
			i++
		default:
			i++
		}
	}
	return len(body)
}

type dollarQuotedHit struct {
	lit   string
	start int
}

// scanDollarQuoted walks `block` and returns every $tag$...$tag$ literal.
// `tag` may be empty (i.e. $$...$$). Implemented manually because Go's RE2
// regex engine does not support backreferences.
func scanDollarQuoted(block string) []dollarQuotedHit {
	out := []dollarQuotedHit{}
	i := 0
	for i < len(block) {
		if block[i] != '$' {
			i++
			continue
		}
		// parse opening tag: $[A-Za-z_][A-Za-z0-9_]*?$
		j := i + 1
		for j < len(block) {
			c := block[j]
			if c == '$' {
				break
			}
			if !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
				(j > i+1 && c >= '0' && c <= '9')) {
				j = -1
				break
			}
			j++
		}
		if j == -1 || j >= len(block) || block[j] != '$' {
			i++
			continue
		}
		tag := block[i : j+1] // includes leading + trailing $
		litStart := j + 1
		// Find closing tag.
		end := strings.Index(block[litStart:], tag)
		if end == -1 {
			i = j + 1
			continue
		}
		out = append(out, dollarQuotedHit{
			lit:   block[litStart : litStart+end],
			start: i,
		})
		i = litStart + end + len(tag)
	}
	return out
}

func isDSLCandidate(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) == 0 {
		return false
	}
	upper := strings.ToUpper(t)
	return strings.HasPrefix(upper, "POLICY ") ||
		strings.HasPrefix(upper, "POLICY\t") ||
		strings.HasPrefix(upper, "POLICY\n") ||
		strings.HasPrefix(upper, "IF ")
}

func lineOf(body string, offset int) int {
	if offset > len(body) {
		offset = len(body)
	}
	line := 1
	for i := 0; i < offset; i++ {
		if body[i] == '\n' {
			line++
		}
	}
	return line
}
