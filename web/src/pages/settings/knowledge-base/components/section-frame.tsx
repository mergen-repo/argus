// FIX-239 DEV-533: section frame shared by all 9 KB sections.

import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import { KB_GROUP_META, type SectionMeta } from '../types'

interface SectionFrameProps {
  meta: SectionMeta
  children: React.ReactNode
}

export function SectionFrame({ meta, children }: SectionFrameProps) {
  const Icon = meta.icon
  const group = KB_GROUP_META[meta.group]
  return (
    <section
      id={meta.id}
      data-kb-section={meta.id}
      data-kb-group={meta.group}
      className="scroll-mt-24 print:break-after-page"
    >
      <Card>
        <div className="flex items-start gap-3 border-b border-border-subtle px-5 py-4">
          <div className={cn('flex h-9 w-9 items-center justify-center rounded-md flex-shrink-0', group.chipClass)}>
            <Icon className="h-4 w-4" />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-mono text-[11px] text-text-tertiary">§{meta.number}</span>
              <h2 className="text-[14px] font-semibold text-text-primary leading-snug">{meta.title}</h2>
              <span
                className={cn(
                  'inline-flex items-center rounded-[var(--radius-sm)] border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide',
                  group.chipClass,
                )}
              >
                {group.label}
              </span>
            </div>
            {meta.subtitle && (
              <p className="text-xs text-text-secondary mt-1 leading-relaxed">{meta.subtitle}</p>
            )}
          </div>
        </div>
        <CardContent className="px-5 py-5">{children}</CardContent>
        <div className="border-t border-border-subtle px-5 py-2 text-[10px] text-text-tertiary font-mono flex items-center justify-between print:hidden">
          <span>Last updated: {meta.lastUpdated}</span>
          <a
            href={`#${meta.id}`}
            className="text-text-tertiary hover:text-accent transition-colors"
            onClick={(e) => {
              e.preventDefault()
              if (navigator.clipboard) {
                const url = `${window.location.origin}${window.location.pathname}#${meta.id}`
                navigator.clipboard.writeText(url)
              }
            }}
          >
            Copy link to §{meta.number}
          </a>
        </div>
      </Card>
    </section>
  )
}
