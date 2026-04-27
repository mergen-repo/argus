// FIX-236 DEV-550: generic virtualised table.
//
// Wraps `@tanstack/react-virtual`'s `useVirtualizer` for any list page that
// can blow past 500 rows (SIMs, Sessions, Audit, Jobs, Violations, Alerts).
// The wrapper is intentionally thin — callers control their own row markup
// via `renderRow`. Common features ship for free: sticky header, sentinel
// row triggering `onLoadMore` for infinite-scroll pages, keyboard nav
// (Home/End/PgUp/PgDn), and a print-mode bypass that disables virtualisation
// so `window.print()` lays every row out on paper.

import * as React from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { cn } from '@/lib/utils'

interface VirtualTableProps<TRow> {
  rows: TRow[]
  rowHeight: number | ((row: TRow, index: number) => number)
  renderRow: (row: TRow, index: number) => React.ReactNode
  header?: React.ReactNode
  /** Rows above + below the visible window kept ready in DOM. Default 8. */
  overscan?: number
  /** Fired when the sentinel near the bottom enters view; gate with hasMore. */
  onLoadMore?: () => void
  hasMore?: boolean
  className?: string
  /** Aria label on the scroll container. */
  ariaLabel?: string
  /** Total height of the scroll viewport. Default '70vh'. */
  height?: string
}

export function VirtualTable<TRow>({
  rows,
  rowHeight,
  renderRow,
  header,
  overscan = 8,
  onLoadMore,
  hasMore,
  className,
  ariaLabel,
  height = '70vh',
}: VirtualTableProps<TRow>) {
  const parentRef = React.useRef<HTMLDivElement>(null)
  const sentinelRef = React.useRef<HTMLDivElement>(null)
  const isPrint = useIsPrintMode()

  const estimateSize = React.useCallback(
    (index: number) => {
      if (typeof rowHeight === 'number') return rowHeight
      const row = rows[index]
      if (!row) return 32
      return rowHeight(row, index)
    },
    [rowHeight, rows],
  )

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize,
    overscan,
  })

  // Sentinel-based onLoadMore — observe the dedicated sentinel div near the
  // bottom of the scroll area; trigger when it enters view and hasMore=true.
  React.useEffect(() => {
    if (!onLoadMore || !hasMore) return
    const el = sentinelRef.current
    if (!el) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) onLoadMore()
      },
      { root: parentRef.current, rootMargin: '0px 0px 200px 0px', threshold: 0 },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [onLoadMore, hasMore])

  // Keyboard navigation: Home/End/PgUp/PgDn at the container level.
  const handleKeyDown = React.useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      const el = parentRef.current
      if (!el) return
      switch (e.key) {
        case 'Home':
          e.preventDefault()
          el.scrollTo({ top: 0 })
          break
        case 'End':
          e.preventDefault()
          el.scrollTo({ top: el.scrollHeight })
          break
        case 'PageUp':
          e.preventDefault()
          el.scrollBy({ top: -el.clientHeight * 0.9 })
          break
        case 'PageDown':
          e.preventDefault()
          el.scrollBy({ top: el.clientHeight * 0.9 })
          break
      }
    },
    [],
  )

  // Print bypass — render every row inline, no virtualisation; browsers
  // can then paginate naturally.
  if (isPrint) {
    return (
      <div className={cn('w-full', className)}>
        {header}
        <div role="table" aria-label={ariaLabel}>
          {rows.map((row, idx) => (
            <React.Fragment key={idx}>{renderRow(row, idx)}</React.Fragment>
          ))}
        </div>
      </div>
    )
  }

  const items = virtualizer.getVirtualItems()
  return (
    <div
      ref={parentRef}
      className={cn('relative overflow-auto outline-none focus:ring-2 focus:ring-accent/40 rounded-[var(--radius-sm)]', className)}
      style={{ height, contain: 'strict' }}
      tabIndex={0}
      role="table"
      aria-label={ariaLabel}
      onKeyDown={handleKeyDown}
    >
      {header && (
        <div className="sticky top-0 z-10 bg-bg-elevated border-b border-border-subtle">
          {header}
        </div>
      )}
      <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
        {items.map((vi) => {
          const row = rows[vi.index]
          if (!row) return null
          return (
            <div
              key={vi.key}
              data-row-index={vi.index}
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                height: vi.size,
                transform: `translateY(${vi.start}px)`,
              }}
            >
              {renderRow(row, vi.index)}
            </div>
          )
        })}
      </div>
      {hasMore && <div ref={sentinelRef} aria-hidden="true" style={{ height: 1 }} />}
    </div>
  )
}

function useIsPrintMode(): boolean {
  const [isPrint, setIsPrint] = React.useState(false)
  React.useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const mq = window.matchMedia('print')
    const update = () => setIsPrint(mq.matches)
    update()
    const handler = (e: MediaQueryListEvent) => setIsPrint(e.matches)
    if (mq.addEventListener) mq.addEventListener('change', handler)
    else mq.addListener(handler)
    return () => {
      if (mq.removeEventListener) mq.removeEventListener('change', handler)
      else mq.removeListener(handler)
    }
  }, [])
  return isPrint
}
