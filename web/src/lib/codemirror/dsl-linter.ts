import { linter, type Diagnostic, type LintSource } from '@codemirror/lint'
import { validateDSL, type DSLValidationError } from '@/lib/api/policies'

function severityToCM(s: DSLValidationError['severity']): Diagnostic['severity'] {
  if (s === 'error') return 'error'
  if (s === 'warning') return 'warning'
  return 'info'
}

export function dslLinterSource(): LintSource {
  return async (view) => {
    const source = view.state.doc.toString()
    if (source.trim() === '') return []
    try {
      const result = await validateDSL(source)
      const diags: DSLValidationError[] = []
      if (result.errors) diags.push(...result.errors)
      if (result.warnings) diags.push(...result.warnings)
      if (diags.length === 0) return []
      const docLines = view.state.doc.lines
      return diags.map((e): Diagnostic => {
        const lineNum = Math.max(1, Math.min(e.line || 1, docLines))
        const lineInfo = view.state.doc.line(lineNum)
        const col = Math.max(0, (e.column || 1) - 1)
        const from = Math.min(lineInfo.from + col, lineInfo.to)
        const to = Math.min(lineInfo.to, from + 1)
        return {
          from,
          to: to > from ? to : from,
          severity: severityToCM(e.severity),
          message: e.code ? `${e.code}: ${e.message}` : e.message,
          source: 'argus-dsl',
        }
      })
    } catch (err) {
      const message = err instanceof Error ? err.message : 'unknown'
      return [
        {
          from: 0,
          to: Math.min(50, view.state.doc.length),
          severity: 'warning',
          message: `validate API error: ${message}`,
          source: 'argus-dsl',
        },
      ]
    }
  }
}

export function dslLinterExtension(debounceMs = 500) {
  return linter(dslLinterSource(), { delay: debounceMs })
}
