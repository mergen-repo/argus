import { StreamLanguage, type StreamParser } from '@codemirror/language'
import {
  autocompletion,
  type CompletionContext,
  type CompletionResult,
} from '@codemirror/autocomplete'
import { fetchVocab } from '@/lib/api/policies'

const keywords = new Set([
  'POLICY', 'MATCH', 'RULES', 'WHEN', 'ACTION', 'CHARGING',
  'IN', 'BETWEEN', 'AND', 'OR', 'NOT',
])

const builtinFunctions = new Set([
  'notify', 'throttle', 'disconnect', 'log', 'block', 'suspend', 'tag',
])

const typeWords = new Set([
  'nb_iot', 'lte_m', 'lte', 'nr_5g',
  'prepaid', 'postpaid', 'hybrid',
  'hourly', 'daily', 'monthly',
  'true', 'false',
  'charge', 'block', 'throttle',
  'mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun',
])

const unitPattern = /^(bps|kbps|mbps|gbps|[KMGT]?B|ms|min|[shd])(?![a-zA-Z_])/i

const operators = new Set(['=', '!=', '>', '>=', '<', '<='])

interface DSLState {
  inString: boolean
  braceDepth: number
}

const dslParser: StreamParser<DSLState> = {
  startState(): DSLState {
    return { inString: false, braceDepth: 0 }
  },

  token(stream, state): string | null {
    if (state.inString) {
      while (!stream.eol()) {
        const ch = stream.next()
        if (ch === '\\') {
          stream.next()
          continue
        }
        if (ch === '"') {
          state.inString = false
          return 'string'
        }
      }
      return 'string'
    }

    if (stream.eatSpace()) return null

    if (stream.match('#')) {
      stream.skipToEnd()
      return 'comment'
    }

    if (stream.match('"')) {
      state.inString = true
      while (!stream.eol()) {
        const ch = stream.next()
        if (ch === '\\') {
          stream.next()
          continue
        }
        if (ch === '"') {
          state.inString = false
          return 'string'
        }
      }
      return 'string'
    }

    if (stream.match(/^-?\d+(\.\d+)?/)) {
      if (stream.match('%')) return 'number'
      stream.match(unitPattern)
      return 'number'
    }

    if (stream.match(/^[!><=]+/)) {
      const matched = stream.current()
      if (operators.has(matched)) return 'operator'
      return null
    }

    if (stream.match('{')) {
      state.braceDepth++
      return 'bracket'
    }
    if (stream.match('}')) {
      state.braceDepth--
      return 'bracket'
    }
    if (stream.match('(') || stream.match(')')) return 'bracket'

    if (stream.match(',')) return 'punctuation'

    if (stream.match(/^[a-zA-Z_][a-zA-Z0-9_.]*/)) {
      const word = stream.current()

      if (keywords.has(word)) return 'keyword'

      if (builtinFunctions.has(word) && stream.peek() === '(') return 'function'

      if (typeWords.has(word.toLowerCase())) return 'typeName'

      if (word === 'rat_type_multiplier') return 'keyword'

      return 'variableName'
    }

    if (stream.match(/^[:\-]/)) return 'punctuation'

    stream.next()
    return null
  },
}

export const dslLanguage = StreamLanguage.define(dslParser)

async function dslCompletions(context: CompletionContext): Promise<CompletionResult | null> {
  const word = context.matchBefore(/\w*/)
  if (!word || (word.from === word.to && !context.explicit)) return null

  const vocab = await fetchVocab()

  const text = context.state.doc.sliceString(0, word.from)
  const lastMatchOpen = text.lastIndexOf('MATCH')
  const lastRulesOpen = text.lastIndexOf('RULES')
  const lastChargingOpen = text.lastIndexOf('CHARGING')
  const lastClose = text.lastIndexOf('}')

  const inMatch = lastMatchOpen > lastClose && /MATCH\s*\{[^}]*$/.test(text)
  const inRules = lastRulesOpen > lastClose && /RULES\s*\{[^}]*$/.test(text)
  const inCharging = lastChargingOpen > lastClose && /CHARGING\s*\{[^}]*$/.test(text)

  let options: { label: string; type: string; detail?: string }[] = []

  if (inMatch) {
    options = vocab.match_fields.map((f) => ({ label: f, type: 'property', detail: 'match field' }))
  } else if (inRules) {
    options = [
      ...vocab.rule_keywords.map((k) => ({ label: k, type: 'keyword', detail: 'rule' })),
      ...(vocab.actions ?? []).map((a) => ({ label: `ACTION ${a}`, type: 'function', detail: 'action' })),
      { label: 'WHEN', type: 'keyword', detail: 'conditional' },
    ]
  } else if (inCharging) {
    options = [
      { label: 'model', type: 'property', detail: 'charging model' },
      { label: 'rate_per_mb', type: 'property' },
      { label: 'rate_per_session', type: 'property' },
      { label: 'billing_cycle', type: 'property' },
      { label: 'quota', type: 'property' },
      { label: 'overage_action', type: 'property' },
      { label: 'overage_rate_per_mb', type: 'property' },
    ]
  } else {
    options = ['POLICY', 'MATCH', 'RULES', 'CHARGING', 'WHEN', 'ACTION', 'IN', 'BETWEEN', 'AND', 'OR', 'NOT'].map(
      (k) => ({ label: k, type: 'keyword' }),
    )
  }

  return { from: word.from, options }
}

export const dslAutocomplete = autocompletion({
  override: [dslCompletions],
  closeOnBlur: false,
})
