// FIX-239 DEV-540: persisted onboarding checklist.
//
// Items toggle independently. Progress persists to localStorage keyed by
// `kb:onboarding:${userId}`. Reset button clears the key. The userId comes
// from the auth store; falls back to 'anon' for unauthenticated previews.

import * as React from 'react'
import { RotateCcw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'

export interface ChecklistItem {
  id: string
  title: string
  desc?: string
}

interface OnboardingChecklistProps {
  items: ChecklistItem[]
  storageKey?: string
}

export function OnboardingChecklist({ items, storageKey = 'kb:onboarding' }: OnboardingChecklistProps) {
  const userId = useAuthStore((s) => s.user?.id ?? 'anon')
  const fullKey = `${storageKey}:${userId}`

  const [checked, setChecked] = React.useState<Set<string>>(() => {
    if (typeof window === 'undefined') return new Set()
    try {
      const raw = window.localStorage.getItem(fullKey)
      if (!raw) return new Set()
      const arr = JSON.parse(raw)
      return new Set(Array.isArray(arr) ? (arr as string[]) : [])
    } catch {
      return new Set()
    }
  })

  React.useEffect(() => {
    try {
      window.localStorage.setItem(fullKey, JSON.stringify([...checked]))
    } catch {
      /* quota / privacy mode — silently noop */
    }
  }, [checked, fullKey])

  const toggle = (id: string) => {
    setChecked((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id); else next.add(id)
      return next
    })
  }

  const reset = () => setChecked(new Set())

  const doneCount = items.filter((i) => checked.has(i.id)).length
  const pct = items.length === 0 ? 0 : Math.round((doneCount / items.length) * 100)

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-2">
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary">Progress</span>
          <span className="font-mono text-xs text-text-primary">{doneCount}/{items.length}</span>
          <span className="font-mono text-xs text-text-tertiary">({pct}%)</span>
        </div>
        <Button variant="ghost" size="sm" onClick={reset} className="gap-1.5 text-[11px] text-text-tertiary hover:text-text-primary">
          <RotateCcw className="h-3 w-3" />
          Reset
        </Button>
      </div>

      <div className="h-1.5 w-full rounded-full bg-bg-hover overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all', pct === 100 ? 'bg-success' : 'bg-accent')}
          style={{ width: `${pct}%` }}
        />
      </div>

      <ul className="space-y-1.5">
        {items.map((item) => {
          const isChecked = checked.has(item.id)
          return (
            <li key={item.id}>
              <label
                className={cn(
                  'flex items-start gap-3 rounded-[var(--radius-sm)] border px-3 py-2 cursor-pointer transition-colors',
                  isChecked ? 'border-success/30 bg-success-dim' : 'border-border-subtle bg-bg-surface hover:bg-bg-hover/50',
                )}
              >
                <Checkbox
                  checked={isChecked}
                  onChange={() => toggle(item.id)}
                  className="mt-0.5 flex-shrink-0"
                  aria-label={`Mark "${item.title}" as done`}
                />
                <span className="flex-1 min-w-0">
                  <span className={cn('block text-xs font-medium', isChecked ? 'line-through text-text-tertiary' : 'text-text-primary')}>
                    {item.title}
                  </span>
                  {item.desc && <span className="block text-[11px] text-text-tertiary mt-0.5">{item.desc}</span>}
                </span>
              </label>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
