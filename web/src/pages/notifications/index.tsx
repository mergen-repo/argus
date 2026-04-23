import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useExport } from '@/hooks/use-export'
import { EmptyState } from '@/components/shared/empty-state'
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
  Download,
  Loader2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import { severityIconClass } from '@/lib/severity'
import { useNotificationList, useMarkAsRead, useMarkAllAsRead, useRealtimeNotifications } from '@/hooks/use-notifications'
import type { Notification } from '@/types/notification'
import { EntityLink } from '@/components/shared/entity-link'
import { NotificationPreferencesPanel } from './preferences-panel'
import { NotificationTemplatesPanel } from './templates-panel'

const categoryIcons: Record<string, React.ElementType> = {
  operator: Building2,
  sim: Smartphone,
  policy: Shield,
  session: Radio,
  system: Server,
  job: ListTodo,
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

type NotificationsTab = 'unread' | 'all' | 'preferences' | 'templates'

export default function NotificationsPage() {
  const [tab, setTab] = useState<NotificationsTab>('all')
  const navigate = useNavigate()

  const inboxTab: 'unread' | 'all' = tab === 'unread' ? 'unread' : 'all'
  const { data: notifications = [] } = useNotificationList(inboxTab)
  useRealtimeNotifications()

  const markAsRead = useMarkAsRead()
  const markAllAsRead = useMarkAllAsRead()
  const { exportCSV, exporting } = useExport('notifications')

  const unreadCount = notifications.filter((n) => !n.read).length
  const filtered = tab === 'unread' ? notifications.filter((n) => !n.read) : notifications

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-text-primary">Notifications</h1>
          <p className="text-sm text-text-secondary">{unreadCount} unread</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => exportCSV()} disabled={exporting} className="gap-1.5">
            {exporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
            Export
          </Button>
          {unreadCount > 0 && (
            <Button variant="outline" size="sm" onClick={() => markAllAsRead.mutate()} className="gap-1.5">
              <CheckCheck className="h-3.5 w-3.5" />
              Mark all as read
            </Button>
          )}
        </div>
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as NotificationsTab)}>
        <TabsList>
          <TabsTrigger value="unread">Unread{unreadCount > 0 && ` (${unreadCount})`}</TabsTrigger>
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="preferences">Preferences</TabsTrigger>
          <TabsTrigger value="templates">Templates</TabsTrigger>
        </TabsList>

        {tab === 'preferences' && (
          <TabsContent value="preferences">
            <NotificationPreferencesPanel />
          </TabsContent>
        )}
        {tab === 'templates' && (
          <TabsContent value="templates">
            <NotificationTemplatesPanel />
          </TabsContent>
        )}

        {(tab === 'unread' || tab === 'all') && (
        <TabsContent value={tab}>
          {filtered.length === 0 ? (
            <EmptyState
              icon={Bell}
              title={tab === 'unread' ? 'No unread notifications' : 'No notifications yet'}
              description={tab === 'unread' ? 'All caught up!' : 'Notifications will appear here when triggered.'}
            />
          ) : (
            <div className="mt-3 space-y-1">
              {filtered.map((n, idx) => {
                const Icon = categoryIcons[n.category] || AlertTriangle
                const navPath = getNavigationPath(n)

                return (
                  <div
                    key={n.id}
                    data-row-index={idx}
                    data-href={navPath || undefined}
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
                    <div className={cn('mt-0.5 shrink-0', severityIconClass(n.severity))}>
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
                      {n.entity_refs && n.entity_refs.length > 0 && (
                        <div
                          className="mt-1.5 flex flex-wrap gap-1.5"
                          onClick={(e) => e.stopPropagation()}
                        >
                          {n.entity_refs.map((ref, i) => (
                            <EntityLink
                              key={`${ref.entity_type}-${ref.entity_id}-${i}`}
                              entityType={ref.entity_type}
                              entityId={ref.entity_id}
                              label={ref.display_name || undefined}
                              showIcon
                            />
                          ))}
                        </div>
                      )}
                    </div>
                    {!n.read && (
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={(e) => {
                          e.stopPropagation()
                          markAsRead.mutate(n.id)
                        }}
                        className="shrink-0 h-7 w-7 text-text-tertiary opacity-0 hover:text-accent group-hover:opacity-100 transition-opacity"
                        title="Mark as read"
                      >
                        <Check className="h-4 w-4" />
                      </Button>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </TabsContent>
        )}
      </Tabs>
    </div>
  )
}
