// FIX-239 DEV-537: minimal inline-SVG sequence diagram.
//
// Pure SVG, no external lib. Suitable for AAA flow / CoA round-trip / SBA call
// patterns. Three message kinds: `sync` (solid arrow), `async` (dashed open
// arrow), `reply` (dashed solid arrow back).

import { cn } from '@/lib/utils'

export interface SeqMessage {
  from: number     // actor index
  to: number       // actor index
  label: string
  kind?: 'sync' | 'async' | 'reply'
}

interface SequenceDiagramProps {
  actors: string[]
  messages: SeqMessage[]
  className?: string
}

const ACTOR_WIDTH = 140
const ROW_HEIGHT = 36
const MARGIN_TOP = 40
const MARGIN_BOTTOM = 16

export function SequenceDiagram({ actors, messages, className }: SequenceDiagramProps) {
  const totalWidth = ACTOR_WIDTH * actors.length
  const totalHeight = MARGIN_TOP + ROW_HEIGHT * messages.length + MARGIN_BOTTOM

  return (
    <div className={cn('overflow-x-auto', className)}>
      <svg
        viewBox={`0 0 ${totalWidth} ${totalHeight}`}
        className="block min-w-[480px]"
        role="img"
        aria-label="Sequence diagram"
      >
        <defs>
          <marker id="kb-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--color-accent)" />
          </marker>
          <marker id="kb-arrow-open" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10" stroke="var(--color-accent)" fill="none" strokeWidth="1.4" />
          </marker>
        </defs>

        {/* Actor labels + lifelines */}
        {actors.map((a, i) => {
          const x = ACTOR_WIDTH * i + ACTOR_WIDTH / 2
          return (
            <g key={`actor-${i}`}>
              <rect
                x={x - 60}
                y={6}
                width={120}
                height={24}
                rx={4}
                ry={4}
                fill="var(--color-bg-elevated)"
                stroke="var(--color-border)"
              />
              <text
                x={x}
                y={22}
                textAnchor="middle"
                fontSize="11"
                fontFamily="var(--font-mono)"
                fill="var(--color-text-primary)"
              >
                {a}
              </text>
              <line
                x1={x}
                x2={x}
                y1={30}
                y2={totalHeight - MARGIN_BOTTOM}
                stroke="var(--color-border-subtle)"
                strokeDasharray="3 3"
              />
            </g>
          )
        })}

        {/* Messages */}
        {messages.map((m, i) => {
          const y = MARGIN_TOP + ROW_HEIGHT * i + ROW_HEIGHT / 2
          const fromX = ACTOR_WIDTH * m.from + ACTOR_WIDTH / 2
          const toX = ACTOR_WIDTH * m.to + ACTOR_WIDTH / 2
          const labelX = (fromX + toX) / 2
          const dash = m.kind === 'reply' || m.kind === 'async' ? '4 3' : ''
          const marker = m.kind === 'async' ? 'url(#kb-arrow-open)' : 'url(#kb-arrow)'
          return (
            <g key={`msg-${i}`}>
              <line
                x1={fromX}
                y1={y}
                x2={toX}
                y2={y}
                stroke="var(--color-accent)"
                strokeWidth="1.4"
                strokeDasharray={dash}
                markerEnd={marker}
              />
              <text
                x={labelX}
                y={y - 6}
                textAnchor="middle"
                fontSize="10"
                fontFamily="var(--font-mono)"
                fill="var(--color-text-secondary)"
              >
                {m.label}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}
