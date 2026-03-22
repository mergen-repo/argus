import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Bell,
  Building2,
  Shield,
  Radio,
  AlertTriangle,
  Server,
  ListTodo,
  Check,
  CheckCheck,
  Smartphone,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import { useNotificationList, useMarkAsRead, useMarkAllAsRead, useRealtimeNotifications } from '@/hooks/use-notifications'
import type { Notification } from '@/types/notification'

const categoryIcons: Record<string, React.ElementType> = {
  operator: Building2,
  sim: Smartphone,
  policy: Shield,
  session: Radio,
  system: Server,
  job: ListTodo,
}

const severityColors: Record<string, string> = {
  info: 'text-accent',
  warning: 'text-warning',
  error: 'text-danger',
  critical: 'text-danger',
}

function getNavigationPath(notification: Notification): string | null {
  if (!notification.resource_type || !notification.resource_id) return null
  const routes: Record<string, string> = {
    operator: '/operators',
    sim: '/sims',
    apn: '/apns',
    policy: '/policies',
    session: '/sessions',
    job: '/jobs',
  }
  const base = routes[notification.resource_type]
  if (!base) return null
  return `${base}/${notification.resource_id}`
}

function formatTimestamp(ts: string): string {
  const date = new Date(ts)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  const diffHr = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHr / 24)

  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHr < 24) return `${diffHr}h ago`
  if (diffDay < 7) return `${diffDay}d ago`
  return date.toLocaleDateString()
}

export default function NotificationsPage() {
  const [tab, setTab] = useState<'unread' | 'all'>('all')
  const navigate = useNavigate()

  const { data: notifications = [] } = useNotificationList(tab)
  useRealtimeNotifications()

  const markAsRead = useMarkAsRead()
  const markAllAsRead = useMarkAllAsRead()

  const unreadCount = notifications.filter((n) => !n.read).length
  const filtered = tab === 'unread' ? notifications.filter((n) => !n.read) : notifications

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-text-primary">Notifications</h1>
          <p className="text-sm text-text-secondary">{unreadCount} unread</p>
        </div>
        {unreadCount > 0 && (
          <Button variant="outline" size="sm" onClick={() => markAllAsRead.mutate()} className="gap-1.5">
            <CheckCheck className="h-3.5 w-3.5" />
            Mark all as read
          </Button>
        )}
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as 'unread' | 'all')}>
        <TabsList>
          <TabsTrigger value="unread">Unread{unreadCount > 0 && ` (${unreadCount})`}</TabsTrigger>
          <TabsTrigger value="all">All</TabsTrigger>
        </TabsList>

        <TabsContent value={tab}>
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center py-16 text-text-tertiary">
              <Bell className="mb-3 h-10 w-10" />
              <p className="text-sm">{tab === 'unread' ? 'No unread notifications' : 'No notifications yet'}</p>
            </div>
          ) : (
            <div className="mt-3 space-y-1">
              {filtered.map((n) => {
                const Icon = categoryIcons[n.category] || AlertTriangle
                const severityColor = severityColors[n.severity] || 'text-text-secondary'
                const navPath = getNavigationPath(n)

                return (
                  <div
                    key={n.id}
                    className={cn(
                      'group flex items-start gap-3 rounded-lg border border-border bg-bg-surface p-4 transition-colors',
                      !n.read && 'border-accent/20 bg-accent-dim/10',
                      navPath && 'cursor-pointer hover:bg-bg-hover',
                    )}
                    onClick={() => {
                      if (!n.read) markAsRead.mutate(n.id)
                      if (navPath) navigate(navPath)
                    }}
                  >
                    <div className={cn('mt-0.5 shrink-0', severityColor)}>
                      <Icon className="h-5 w-5" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-start justify-between gap-2">
                        <p className={cn('text-sm', !n.read ? 'font-medium text-text-primary' : 'text-text-secondary')}>
                          {n.title}
                        </p>
                        <span className="shrink-0 text-xs text-text-tertiary">{formatTimestamp(n.created_at)}</span>
                      </div>
                      <p className="mt-0.5 text-xs text-text-tertiary">{n.message}</p>
                    </div>
                    {!n.read && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          markAsRead.mutate(n.id)
                        }}
                        className="shrink-0 rounded p-1 text-text-tertiary opacity-0 hover:text-accent group-hover:opacity-100 transition-opacity"
                        title="Mark as read"
                      >
                        <Check className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}
