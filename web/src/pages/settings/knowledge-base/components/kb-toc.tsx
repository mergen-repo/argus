// FIX-239 DEV-534: sticky left TOC for the Knowledge Base.
//
// Highlights the section currently in viewport via IntersectionObserver.
// Smooth-scrolls to anchors on click. Hidden in print.

import * as React from 'react'
import { cn } from '@/lib/utils'
import { KB_GROUP_META, type SectionModule } from '../types'

interface KbTocProps {
  sections: SectionModule[]
  activeId: string | null
  onJump: (id: string) => void
}

export function KbToc({ sections, activeId, onJump }: KbTocProps) {
  return (
    <nav
      aria-label="Knowledge base sections"
      className="sticky top-4 max-h-[calc(100vh-2rem)] overflow-y-auto pr-2 print:hidden"
    >
      <p className="text-[10px] uppercase tracking-wider text-text-tertiary mb-2 px-2">Sections</p>
      <ol className="space-y-0.5">
        {sections.map((s) => {
          const group = KB_GROUP_META[s.meta.group]
          const isActive = activeId === s.meta.id
          return (
            <li key={s.meta.id}>
              <a
                href={`#${s.meta.id}`}
                className={cn(
                  'flex items-start gap-2 rounded-[var(--radius-sm)] border-l-2 pl-2 pr-2 py-1.5 transition-colors text-left',
                  group.sidebarClass,
                  isActive
                    ? 'bg-bg-hover text-text-primary'
                    : 'text-text-secondary hover:bg-bg-hover/50 hover:text-text-primary',
                )}
                onClick={(e) => {
                  e.preventDefault()
                  onJump(s.meta.id)
                }}
              >
                <span className="font-mono text-[10px] text-text-tertiary shrink-0 w-5">§{s.meta.number}</span>
                <span className="text-xs leading-snug">{s.meta.title}</span>
              </a>
            </li>
          )
        })}
      </ol>
    </nav>
  )
}

/**
 * Hook returning the id of the section currently nearest the top of the
 * scroll viewport. Recomputes on intersection events; throttled by the
 * browser's IntersectionObserver delivery.
 */
export function useActiveSection(sectionIds: string[]): string | null {
  const [activeId, setActiveId] = React.useState<string | null>(sectionIds[0] ?? null)

  React.useEffect(() => {
    if (sectionIds.length === 0) return
    const observers: IntersectionObserver[] = []
    const visibility = new Map<string, number>()

    const recompute = () => {
      let bestId: string | null = null
      let bestRatio = 0
      visibility.forEach((ratio, id) => {
        if (ratio > bestRatio) {
          bestRatio = ratio
          bestId = id
        }
      })
      if (bestId) setActiveId(bestId)
    }

    sectionIds.forEach((id) => {
      const el = document.getElementById(id)
      if (!el) return
      const observer = new IntersectionObserver(
        (entries) => {
          entries.forEach((entry) => {
            visibility.set(id, entry.intersectionRatio)
          })
          recompute()
        },
        { threshold: [0, 0.25, 0.5, 0.75, 1], rootMargin: '-80px 0px -40% 0px' },
      )
      observer.observe(el)
      observers.push(observer)
    })
    return () => observers.forEach((o) => o.disconnect())
  }, [sectionIds])

  return activeId
}
