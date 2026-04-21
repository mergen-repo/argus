import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Activity, Pause, Play, Radio, Trash2 } from 'lucide-react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Sheet, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  useEventStore,
  useFilteredEventsSelector,
  type LiveEvent,
} from '@/stores/events'
import { EventFilterBar } from './event-filter-bar'
import { EventRow } from './event-row'

const VIRTUALIZE_THRESHOLD = 100
const RELATIVE_TIME_REFRESH_MS = 15_000

function activeFilterCount(
  f: ReturnType<typeof useEventStore.getState>['filters'],
): number {
  let n = 0
  if (f.types.length > 0) n++
  if (f.severities.length > 0) n++
  if (f.entityTypes.length > 0) n++
  if (f.sources.length > 0) n++
  if (f.dateRange !== 'session') n++
  return n
}

export function EventStreamDrawer() {
  const drawerOpen = useEventStore((s) => s.drawerOpen)
  const setDrawerOpen = useEventStore((s) => s.setDrawerOpen)
  const paused = useEventStore((s) => s.paused)
  const setPaused = useEventStore((s) => s.setPaused)
  const resumeAndFlush = useEventStore((s) => s.resumeAndFlush)
  const clearEvents = useEventStore((s) => s.clearEvents)
  const filters = useEventStore((s) => s.filters)
  const queuedCount = useEventStore((s) => s.queuedEvents.length)
  const totalEvents = useEventStore((s) => s.events.length)
  const filteredEvents = useEventStore(useFilteredEventsSelector)

  // 15s tick forces relative-time re-render. Scoped to drawer-open so CPU
  // is silent when drawer is closed. StrictMode double-mount safe.
  const [, setTick] = useState(0)
  useEffect(() => {
    if (!drawerOpen) return
    const id = setInterval(() => setTick((t) => t + 1), RELATIVE_TIME_REFRESH_MS)
    return () => clearInterval(id)
  }, [drawerOpen])

  const close = useCallback(() => setDrawerOpen(false), [setDrawerOpen])

  const activeFilters = useMemo(() => activeFilterCount(filters), [filters])

  return (
    <Sheet open={drawerOpen} onOpenChange={setDrawerOpen} side="right">
      <SheetHeader>
        <div className="flex items-center justify-between">
          <SheetTitle className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-accent" />
            Canlı Olay Akışı
          </SheetTitle>
          <span
            role="status"
            aria-live="polite"
            className="flex items-center gap-1.5"
          >
            <span
              className={cn(
                'h-1.5 w-1.5 rounded-full',
                paused ? 'bg-warning' : 'bg-success pulse-dot shadow-[var(--shadow-glow-success)]',
              )}
            />
            <span
              className={cn(
                'text-[10px] font-semibold tracking-wider',
                paused ? 'text-warning' : 'text-success',
              )}
            >
              {paused ? 'DURAKLATILDI' : 'LIVE'}
            </span>
          </span>
        </div>
      </SheetHeader>

      <div className="mb-2 flex items-center justify-between gap-2 px-1">
        <div className="text-[10px] text-text-tertiary">
          {filteredEvents.length} / {totalEvents} olay
          {activeFilters > 0 ? ` · ${activeFilters} filtre aktif` : ''}
        </div>
        <div className="flex items-center gap-1.5">
          {queuedCount > 0 && (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              onClick={resumeAndFlush}
              aria-label={`${queuedCount} yeni kuyrukta bekleyen olay, devam etmek için tıklayın`}
              className="bg-accent/15 text-accent hover:bg-accent/25 animate-pulse font-semibold"
            >
              {queuedCount} yeni olay
            </Button>
          )}
          {paused ? (
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={resumeAndFlush}
              aria-label="Olay akışını devam ettir"
              title="Devam Et"
            >
              <Play className="h-3 w-3" />
              Devam
            </Button>
          ) : (
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() => setPaused(true)}
              aria-label="Olay akışını duraklat"
              title="Duraklat"
            >
              <Pause className="h-3 w-3" />
              Duraklat
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="xs"
            onClick={clearEvents}
            aria-label="Olay listesini ve filtreleri temizle"
            title="Temizle"
            className="hover:text-danger hover:border-danger/40 hover:bg-danger/10"
          >
            <Trash2 className="h-3 w-3" />
            Temizle
          </Button>
        </div>
      </div>

      <EventList events={filteredEvents} totalEvents={totalEvents} onClose={close} />
    </Sheet>
  )
}

interface EventListProps {
  events: LiveEvent[]
  totalEvents: number
  onClose: () => void
}

function EventList({ events, totalEvents, onClose }: EventListProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null)

  const virtualizer = useVirtualizer({
    count: events.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 88,
    overscan: 5,
    getItemKey: (i) => events[i]?.id ?? i,
  })

  const shouldVirtualize = events.length > VIRTUALIZE_THRESHOLD

  if (totalEvents === 0) {
    return (
      <div className="flex h-[200px] flex-col items-center justify-center gap-2 text-sm text-text-tertiary">
        <Radio className="h-5 w-5 animate-pulse" />
        <span>Olay bekleniyor...</span>
      </div>
    )
  }

  // F-A1: filter bar lives INSIDE the scroll container so sticky positioning
  // resolves against the same overflow ancestor the event list scrolls within.
  return (
    <div
      ref={scrollRef}
      className="overflow-y-auto max-h-[calc(100vh-220px)]"
    >
      <EventFilterBar />

      {events.length === 0 ? (
        <div className="flex h-[180px] flex-col items-center justify-center gap-2 text-sm text-text-tertiary">
          <span>Filtre eşleşmesi yok</span>
          <span className="text-[11px] text-text-tertiary opacity-70">
            Seçili filtreleri kaldırın veya Temizle düğmesini kullanın.
          </span>
        </div>
      ) : !shouldVirtualize ? (
        <div className="flex flex-col gap-0.5">
          {events.map((event, idx) => (
            <EventRow key={event.id} event={event} onClose={onClose} isFirst={idx === 0} />
          ))}
        </div>
      ) : (
        <div style={{ height: virtualizer.getTotalSize(), position: 'relative', width: '100%' }}>
          {virtualizer.getVirtualItems().map((v) => {
            const event = events[v.index]
            if (!event) return null
            return (
              <div
                key={v.key}
                ref={virtualizer.measureElement}
                data-index={v.index}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  transform: `translateY(${v.start}px)`,
                }}
              >
                <EventRow event={event} onClose={onClose} isFirst={v.index === 0} />
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
