// FIX-239: Knowledge Base — Ops Runbook (9-section restructure).
//
// Top-level page orchestrator. Composes:
//   - Sticky left TOC (KbToc) with active-section highlighting
//   - Right column: 9 section frames rendered in registry order
//   - Cmd+K KbSearch overlay (route-scoped — see useKbHotkey)
//   - Export-as-PDF trigger → window.print() (Wave A print CSS handles layout)
//
// Section content + components live under
// `web/src/pages/settings/knowledge-base/`. This file stays thin.

import * as React from 'react'
import { BookOpen, Printer, Search, Command as CmdIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { KB_SECTIONS, KB_SECTION_IDS } from './knowledge-base/registry'
import { SectionFrame } from './knowledge-base/components/section-frame'
import { KbToc, useActiveSection } from './knowledge-base/components/kb-toc'
import { KbSearch } from './knowledge-base/components/kb-search'

export default function KnowledgeBasePage() {
  const [searchOpen, setSearchOpen] = React.useState(false)
  const activeId = useActiveSection(KB_SECTION_IDS)

  const handleJump = React.useCallback((anchor: string) => {
    const el = document.getElementById(anchor)
    if (!el) return
    el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    if (history.replaceState) {
      history.replaceState(null, '', `#${anchor}`)
    } else {
      window.location.hash = anchor
    }
  }, [])

  // Cmd+K / Ctrl+K opens the KB search overlay; only when this page is mounted
  // (this effect is scoped by component lifecycle).
  React.useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setSearchOpen((prev) => !prev)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  // Honor incoming hash on mount — deep-link to a section.
  React.useEffect(() => {
    const hash = window.location.hash.replace(/^#/, '')
    if (!hash) return
    // Defer so the section DOM has mounted.
    const t = window.setTimeout(() => {
      const el = document.getElementById(hash)
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }, 100)
    return () => window.clearTimeout(t)
  }, [])

  return (
    <div className="space-y-5">
      <div className="print:hidden">
        <Breadcrumb items={[{ label: 'Settings', href: '/settings' }, { label: 'Knowledge Base' }]} className="mb-2" />
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-3">
            <BookOpen className="h-5 w-5 text-accent" />
            <div>
              <h1 className="text-[16px] font-semibold text-text-primary">Knowledge Base — Ops Runbook</h1>
              <p className="text-xs text-text-secondary mt-0.5">
                9 sections covering onboarding, operations, troubleshooting, and protocol reference.
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              className="gap-2 text-xs"
              onClick={() => setSearchOpen(true)}
              aria-label="Open Knowledge Base search"
            >
              <Search className="h-3.5 w-3.5" />
              <span>Search…</span>
              <kbd className="text-[10px] font-mono text-text-tertiary border border-border-subtle rounded px-1 py-0.5 inline-flex items-center gap-0.5">
                <CmdIcon className="h-2.5 w-2.5" />K
              </kbd>
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5 text-xs"
              onClick={() => window.print()}
              aria-label="Export Knowledge Base as PDF"
            >
              <Printer className="h-3.5 w-3.5" />
              Export PDF
            </Button>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[220px_minmax(0,1fr)] gap-6">
        <div className="hidden lg:block">
          <KbToc sections={KB_SECTIONS} activeId={activeId} onJump={handleJump} />
        </div>
        <div className="space-y-5 print:space-y-0">
          {KB_SECTIONS.map((s) => (
            <SectionFrame key={s.meta.id} meta={s.meta}>
              <s.Component />
            </SectionFrame>
          ))}
        </div>
      </div>

      <KbSearch
        open={searchOpen}
        onOpenChange={setSearchOpen}
        sections={KB_SECTIONS}
        onJump={handleJump}
      />
    </div>
  )
}
