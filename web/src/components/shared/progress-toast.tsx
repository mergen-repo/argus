import * as React from 'react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'

interface ProgressToastProps {
  jobId: string
  label: string
}

export const ProgressToast = React.memo(function ProgressToast({
  jobId,
  label,
}: ProgressToastProps) {
  const [percent, setPercent] = React.useState(0)
  const toastIdRef = React.useRef<string | number | null>(null)

  React.useEffect(() => {
    toastIdRef.current = toast.loading(
      <ProgressContent label={label} percent={percent} />,
      { duration: Infinity },
    )
    return () => {
      if (toastIdRef.current != null) toast.dismiss(toastIdRef.current)
    }
  }, [])

  React.useEffect(() => {
    if (toastIdRef.current == null) return
    toast.loading(<ProgressContent label={label} percent={percent} />, {
      id: toastIdRef.current,
      duration: percent >= 100 ? 3000 : Infinity,
    })
    if (percent >= 100) {
      setTimeout(() => {
        toast.success(label + ' completed', { id: toastIdRef.current! })
      }, 500)
    }
  }, [percent, label])

  return null
})

function ProgressContent({ label, percent }: { label: string; percent: number }) {
  return (
    <div className="flex flex-col gap-1.5 w-48">
      <span className="text-xs text-text-primary font-medium">{label}</span>
      <div className="h-1.5 w-full rounded-full bg-bg-hover overflow-hidden">
        <div
          className="h-full rounded-full bg-accent transition-all duration-300"
          style={{ width: `${Math.min(100, percent)}%` }}
        />
      </div>
      <span className="text-[10px] text-text-tertiary text-right">{percent}%</span>
    </div>
  )
}
