import { cn } from '@/lib/utils'

interface InfoRowProps {
  label: string
  value: string | React.ReactNode
  mono?: boolean
}

export function InfoRow({ label, value, mono }: InfoRowProps) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-xs text-text-secondary">{label}</span>
      <span className={cn('text-sm text-text-primary', mono && 'font-mono text-xs')}>
        {value}
      </span>
    </div>
  )
}
