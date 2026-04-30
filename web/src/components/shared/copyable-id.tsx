import * as React from 'react'
import { Copy, Check } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Tooltip } from '@/components/ui/tooltip'
import { Button } from '@/components/ui/button'

interface CopyableIdProps {
  value: string
  label?: string
  masked?: boolean
  mono?: boolean
  className?: string
}

function maskValue(value: string): string {
  if (value.length <= 8) return value
  return `${value.slice(0, 4)}•••${value.slice(-4)}`
}

export const CopyableId = React.memo(function CopyableId({
  value,
  label,
  masked = false,
  mono = true,
  className,
}: CopyableIdProps) {
  const [copied, setCopied] = React.useState(false)
  const [revealed, setRevealed] = React.useState(false)

  const displayValue = masked && !revealed ? maskValue(value) : value

  function handleCopy(e: React.MouseEvent) {
    e.stopPropagation()
    if (masked && !revealed) {
      setRevealed(true)
      return
    }
    navigator.clipboard.writeText(value).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  const textEl = (
    <span
      className={cn(
        'select-all',
        mono ? 'font-mono text-[12px]' : 'text-[13px]',
        'text-text-primary',
        className,
      )}
    >
      {label ? `${label}: ` : ''}
      {displayValue}
    </span>
  )

  return (
    <div className="inline-flex items-center gap-1.5 group" role="group" aria-label={`Copy ${label ?? 'ID'}`}>
      {textEl}
      <Tooltip content={copied ? 'Copied!' : masked && !revealed ? 'Click to reveal' : 'Copy to clipboard'}>
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 opacity-0 group-hover:opacity-100 transition-opacity duration-200"
          onClick={handleCopy}
          aria-label={copied ? 'Copied' : 'Copy'}
        >
          {copied ? (
            <Check className="h-3 w-3 text-success" />
          ) : (
            <Copy className="h-3 w-3 text-text-tertiary" />
          )}
        </Button>
      </Tooltip>
    </div>
  )
})
