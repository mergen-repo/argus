import { RAT_DISPLAY } from '@/lib/constants'

interface RATBadgeProps {
  ratType?: string | null
}

export function RATBadge({ ratType }: RATBadgeProps) {
  if (!ratType) return <span className="text-text-tertiary text-xs">-</span>
  return (
    <span className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium">
      {RAT_DISPLAY[ratType] ?? ratType}
    </span>
  )
}
