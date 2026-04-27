// FIX-239 DEV-545: Cmd+K search overlay scoped to the Knowledge Base.
//
// Activates only when the KB route is mounted (the parent attaches the
// keydown listener). Builds an in-memory index from each section's
// `meta.searchTerms` plus h2/h3 headings inside the section DOM.

import * as React from 'react'
import { Command } from 'cmdk'
import { Search, ArrowRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { KB_GROUP_META, type SectionModule } from '../types'

interface KbSearchProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  sections: SectionModule[]
  onJump: (anchor: string) => void
}

interface IndexEntry {
  sectionId: string
  sectionTitle: string
  sectionNumber: number
  group: keyof typeof KB_GROUP_META
  /** Either the section title itself or a heading inside it. */
  heading: string
  /** Anchor target — section id, optionally `${section}-${slug}` if a heading. */
  anchor: string
  searchKey: string
}

function slug(text: string): string {
  return text.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '')
}

function buildIndex(sections: SectionModule[]): IndexEntry[] {
  const entries: IndexEntry[] = []
  sections.forEach((s) => {
    entries.push({
      sectionId: s.meta.id,
      sectionTitle: s.meta.title,
      sectionNumber: s.meta.number,
      group: s.meta.group,
      heading: s.meta.title,
      anchor: s.meta.id,
      searchKey: [s.meta.title, ...s.meta.searchTerms].join(' '),
    })
    if (typeof document !== 'undefined') {
      const sectionEl = document.getElementById(s.meta.id)
      if (sectionEl) {
        sectionEl.querySelectorAll<HTMLElement>('h3, h4').forEach((h) => {
          const text = h.textContent?.trim()
          if (!text) return
          const headingId = h.id || `${s.meta.id}-${slug(text)}`
          if (!h.id) h.id = headingId
          entries.push({
            sectionId: s.meta.id,
            sectionTitle: s.meta.title,
            sectionNumber: s.meta.number,
            group: s.meta.group,
            heading: text,
            anchor: headingId,
            searchKey: `${s.meta.title} ${text}`,
          })
        })
      }
    }
  })
  return entries
}

export function KbSearch({ open, onOpenChange, sections, onJump }: KbSearchProps) {
  const [query, setQuery] = React.useState('')
  const [index, setIndex] = React.useState<IndexEntry[]>([])

  // Rebuild index whenever the panel opens — picks up section DOM mutations.
  React.useEffect(() => {
    if (open) setIndex(buildIndex(sections))
    else setQuery('')
  }, [open, sections])

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-label="Search knowledge base"
      className="fixed inset-0 z-50 flex items-start justify-center pt-[12vh] px-4 print:hidden"
    >
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => onOpenChange(false)} />
      <div className="relative z-10 w-full max-w-xl rounded-[var(--radius-lg)] border border-border-default bg-bg-elevated shadow-2xl overflow-hidden">
        <Command label="Knowledge Base search" loop className="bg-bg-elevated">
          <div className="flex items-center gap-2 border-b border-border-subtle px-4 py-3">
            <Search className="h-4 w-4 text-text-tertiary" />
            <Command.Input
              placeholder="Search sections, ports, operations…"
              value={query}
              onValueChange={setQuery}
              autoFocus
              className="flex-1 bg-transparent text-sm text-text-primary placeholder:text-text-tertiary focus:outline-none"
            />
            <kbd className="text-[10px] font-mono text-text-tertiary border border-border-subtle rounded px-1.5 py-0.5">esc</kbd>
          </div>
          <Command.List className="max-h-[55vh] overflow-y-auto py-2">
            <Command.Empty className="px-4 py-6 text-center text-xs text-text-tertiary">
              No matches.
            </Command.Empty>
            {index.map((entry) => {
              const group = KB_GROUP_META[entry.group]
              return (
                <Command.Item
                  key={`${entry.anchor}-${entry.heading}`}
                  value={entry.searchKey}
                  onSelect={() => {
                    onJump(entry.anchor)
                    onOpenChange(false)
                  }}
                  className="cursor-pointer flex items-center gap-3 px-4 py-2 text-xs aria-selected:bg-bg-hover"
                >
                  <span
                    className={cn(
                      'inline-flex shrink-0 items-center rounded-[var(--radius-sm)] border px-1.5 py-0.5 text-[9px] font-medium uppercase tracking-wider',
                      group.chipClass,
                    )}
                  >
                    §{entry.sectionNumber}
                  </span>
                  <span className="text-text-primary truncate flex-1">{entry.heading}</span>
                  {entry.heading !== entry.sectionTitle && (
                    <span className="text-text-tertiary text-[10px] truncate">in {entry.sectionTitle}</span>
                  )}
                  <ArrowRight className="h-3 w-3 text-text-tertiary shrink-0" />
                </Command.Item>
              )
            })}
          </Command.List>
        </Command>
      </div>
    </div>
  )
}
