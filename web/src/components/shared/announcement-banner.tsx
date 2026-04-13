import * as React from 'react'
import { X, AlertTriangle, Info, AlertOctagon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useAnnouncements } from '@/hooks/use-announcements'
import type { Announcement } from '@/hooks/use-announcements'
import { cn } from '@/lib/utils'

const TYPE_CONFIG = {
  info: {
    icon: Info,
    classes: 'bg-info/10 border-info text-info',
  },
  warning: {
    icon: AlertTriangle,
    classes: 'bg-warning-dim border-warning text-warning',
  },
  critical: {
    icon: AlertOctagon,
    classes: 'bg-danger-dim border-danger text-danger',
  },
}

interface AnnouncementItemProps {
  announcement: Announcement
  onDismiss: (id: string) => void
}

const AnnouncementItem = React.memo(function AnnouncementItem({
  announcement: a,
  onDismiss,
}: AnnouncementItemProps) {
  const cfg = TYPE_CONFIG[a.type]
  const Icon = cfg.icon

  return (
    <div className={cn('flex items-center justify-between gap-3 px-4 py-2 border-b text-xs', cfg.classes)}>
      <span className="flex items-center gap-2">
        <Icon className="h-3.5 w-3.5 flex-shrink-0" />
        <span className="font-medium">{a.title}</span>
        {a.body && <span className="text-text-secondary hidden sm:inline">{a.body}</span>}
      </span>
      {a.dismissible && (
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 opacity-70 hover:opacity-100"
          onClick={() => onDismiss(a.id)}
          aria-label="Dismiss"
        >
          <X className="h-3 w-3" />
        </Button>
      )}
    </div>
  )
})

export const AnnouncementBanner = React.memo(function AnnouncementBanner() {
  const { data: announcements = [], dismiss } = useAnnouncements()

  if (announcements.length === 0) return null

  return (
    <div className="w-full">
      {announcements.map((a) => (
        <AnnouncementItem key={a.id} announcement={a} onDismiss={(id) => dismiss.mutate(id)} />
      ))}
    </div>
  )
})
