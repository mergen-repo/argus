// FIX-239 DEV-538: SlidePanel-based request/response popup with three tabs.
//
// Tabs: Wire Format (hex/AVP), curl one-liner, Expected Response.
// Reuses FIX-216 SlidePanel pattern. Toggle "Show wire format" hides the
// hex tab for non-experts (defaults to on for protocol engineers).

import * as React from 'react'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { cn } from '@/lib/utils'
import { Copy, Check, ExternalLink } from 'lucide-react'
import { toast } from 'sonner'

type TabKey = 'wire' | 'curl' | 'response'

export interface RequestResponseExample {
  title: string
  /** One-liner subtitle below the title (e.g. RFC reference). */
  reference?: string
  wire: string          // hex dump or AVP table as text
  curl: string          // single shell command
  response: string      // expected response body
  /** Optional deep link target — populates "Try in Live Tester" button. */
  liveTesterUrl?: string
}

interface RequestResponsePopupProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  example: RequestResponseExample
}

export function RequestResponsePopup({ open, onOpenChange, example }: RequestResponsePopupProps) {
  const [tab, setTab] = React.useState<TabKey>('wire')
  const [showWire, setShowWire] = React.useState(true)

  React.useEffect(() => {
    if (!showWire && tab === 'wire') setTab('curl')
  }, [showWire, tab])

  React.useEffect(() => {
    if (!open) setTab('wire')
  }, [open])

  const tabs: { key: TabKey; label: string; visible: boolean }[] = [
    { key: 'wire', label: 'Wire Format', visible: showWire },
    { key: 'curl', label: 'curl', visible: true },
    { key: 'response', label: 'Expected Response', visible: true },
  ]

  const activeBody = tab === 'wire' ? example.wire : tab === 'curl' ? example.curl : example.response

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(activeBody)
      toast.success('Copied to clipboard')
    } catch {
      toast.error('Copy failed')
    }
  }

  return (
    <SlidePanel
      open={open}
      onOpenChange={onOpenChange}
      title={example.title}
      description={example.reference}
      width="lg"
    >
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div role="tablist" className="flex items-center gap-1 rounded-[var(--radius-sm)] border border-border-subtle bg-bg-elevated p-0.5">
            {tabs.filter((t) => t.visible).map((t) => (
              <Button
                key={t.key}
                role="tab"
                aria-selected={tab === t.key}
                onClick={() => setTab(t.key)}
                variant="ghost"
                size="sm"
                className={cn(
                  'rounded-[var(--radius-sm)] px-3 py-1 text-xs font-medium h-auto',
                  tab === t.key
                    ? 'bg-bg-surface text-text-primary'
                    : 'text-text-tertiary hover:text-text-primary',
                )}
              >
                {t.label}
              </Button>
            ))}
          </div>
          <label className="flex items-center gap-2 text-[10px] text-text-tertiary cursor-pointer select-none">
            <Checkbox
              checked={showWire}
              onChange={(e) => setShowWire((e.target as HTMLInputElement).checked)}
              aria-label="Show wire format tab"
              className="h-3 w-3"
            />
            Show wire format
          </label>
        </div>

        <pre className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-primary px-4 py-3 text-[11px] font-mono text-text-primary overflow-x-auto whitespace-pre leading-relaxed">
          {activeBody}
        </pre>
      </div>
      <SlidePanelFooter>
        <Button variant="ghost" size="sm" className="gap-1.5" onClick={handleCopy}>
          <Copy className="h-3.5 w-3.5" />
          Copy
        </Button>
        {example.liveTesterUrl && (
          <a
            href={example.liveTesterUrl}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] border border-border-default bg-bg-surface px-3 py-1.5 text-xs text-text-primary hover:bg-bg-hover transition-colors"
          >
            <ExternalLink className="h-3.5 w-3.5" />
            Try in Live Tester
          </a>
        )}
        <Button variant="default" size="sm" className="gap-1.5" onClick={() => onOpenChange(false)}>
          <Check className="h-3.5 w-3.5" />
          Done
        </Button>
      </SlidePanelFooter>
    </SlidePanel>
  )
}
