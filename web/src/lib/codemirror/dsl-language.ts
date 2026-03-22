import { StreamLanguage, type StreamParser } from '@codemirror/language'

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
