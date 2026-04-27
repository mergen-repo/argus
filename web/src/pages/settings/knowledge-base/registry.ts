// FIX-239 DEV-533: Knowledge Base section registry — order matters.
//
// To add or rename a section: import the meta + Component, append a new
// SectionModule entry below. The id field is the URL anchor and MUST be
// stable; renaming breaks deep links.

import { meta as m1, Component as C1 } from './sections/section-1-operator-onboarding'
import { meta as m2, Component as C2 } from './sections/section-2-aaa-business-flow'
import { meta as m3, Component as C3 } from './sections/section-3-session-lifecycle'
import { meta as m4, Component as C4 } from './sections/section-4-policy-workflow'
import { meta as m5, Component as C5 } from './sections/section-5-ip-allocation-apn-types'
import { meta as m6, Component as C6 } from './sections/section-6-operator-integration-runbook'
import { meta as m7, Component as C7 } from './sections/section-7-common-operations-cookbook'
import { meta as m8, Component as C8 } from './sections/section-8-troubleshooting-playbooks'
import { meta as m9, Component as C9 } from './sections/section-9-business-rules-reference'
import type { SectionModule } from './types'

export const KB_SECTIONS: SectionModule[] = [
  { meta: m1, Component: C1 },
  { meta: m2, Component: C2 },
  { meta: m3, Component: C3 },
  { meta: m4, Component: C4 },
  { meta: m5, Component: C5 },
  { meta: m6, Component: C6 },
  { meta: m7, Component: C7 },
  { meta: m8, Component: C8 },
  { meta: m9, Component: C9 },
]

export const KB_SECTION_IDS: string[] = KB_SECTIONS.map((s) => s.meta.id)
