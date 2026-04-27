// FIX-239 DEV-533: Knowledge Base section module shape.

import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'

export type KbGroup = 'onboarding' | 'operations' | 'troubleshooting' | 'reference'

export interface SectionMeta {
  /** URL anchor without the leading '#'. Stable; do not rename without redirects. */
  id: string
  /** Display number 1..9. */
  number: number
  title: string
  subtitle?: string
  group: KbGroup
  icon: LucideIcon
  /** Free-text terms boosted in Cmd+K search. */
  searchTerms: string[]
  /** YYYY-MM-DD literal — surfaces in section footer. */
  lastUpdated: string
}

export interface SectionModule {
  meta: SectionMeta
  /** Section body component. */
  Component: () => ReactNode
}

export const KB_GROUP_META: Record<KbGroup, { label: string; chipClass: string; sidebarClass: string }> = {
  onboarding: {
    label: 'Onboarding',
    chipClass: 'border-accent/30 bg-accent-dim text-accent',
    sidebarClass: 'border-l-accent',
  },
  operations: {
    label: 'Operations',
    chipClass: 'border-success/30 bg-success-dim text-success',
    sidebarClass: 'border-l-success',
  },
  troubleshooting: {
    label: 'Troubleshooting',
    chipClass: 'border-warning/30 bg-warning-dim text-warning',
    sidebarClass: 'border-l-warning',
  },
  reference: {
    label: 'Reference',
    chipClass: 'border-border-default bg-bg-elevated text-text-secondary',
    sidebarClass: 'border-l-border-default',
  },
}
