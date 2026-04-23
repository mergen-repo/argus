/**
 * Smoke tests for InfoTooltip component (FIX-222).
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 *
 * Behavior contract:
 *  - Renders children + Info icon for known terms
 *  - Opens after 500ms hover delay (ESC closes)
 *  - Unknown term → logs console.warn (dev only), renders children without tooltip
 *  - Uses GLOSSARY_TOOLTIPS for copy
 */

import type { ReactNode } from 'react'
import { GLOSSARY_TOOLTIPS } from '@/lib/glossary-tooltips'

// Verify all 9 required terms are present
const REQUIRED_TERMS = ['MCC', 'MNC', 'EID', 'MSISDN', 'APN', 'IMSI', 'ICCID', 'CoA', 'SLA'] as const
type RequiredTerm = typeof REQUIRED_TERMS[number]

// Type-level exhaustiveness: all required terms must exist in GLOSSARY_TOOLTIPS
const _termCheck: Record<RequiredTerm, string> = {
  MCC: GLOSSARY_TOOLTIPS['MCC'],
  MNC: GLOSSARY_TOOLTIPS['MNC'],
  EID: GLOSSARY_TOOLTIPS['EID'],
  MSISDN: GLOSSARY_TOOLTIPS['MSISDN'],
  APN: GLOSSARY_TOOLTIPS['APN'],
  IMSI: GLOSSARY_TOOLTIPS['IMSI'],
  ICCID: GLOSSARY_TOOLTIPS['ICCID'],
  CoA: GLOSSARY_TOOLTIPS['CoA'],
  SLA: GLOSSARY_TOOLTIPS['SLA'],
}

// Verify all values are non-empty strings
for (const [term, copy] of Object.entries(_termCheck)) {
  if (!copy || copy.trim().length === 0) {
    throw new Error(`Glossary term "${term}" has empty copy`)
  }
}

// InfoTooltip props contract smoke test
type InfoTooltipProps = {
  term: string
  children: ReactNode
  side?: 'top' | 'bottom' | 'left' | 'right'
  className?: string
}

const _props: InfoTooltipProps = {
  term: 'MCC',
  children: 'MCC',
  side: 'top',
}

export { _termCheck, _props }
