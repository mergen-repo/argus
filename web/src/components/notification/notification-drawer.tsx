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
import { Sheet, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useNotificationStore } from '@/stores/notification'
import { severityIconClass } from '@/lib/severity'
import { useNotificationList, useMarkAsRead, useMarkAllAsRead, useUnreadCount, useRealtimeNotifications } from '@/hooks/use-notifications'
import type { Notification } from '@/types/notification'

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

function NotificationItem({
  notification,
  onRead,
  onNavigate,
}: {
  notification: Notification
  onRead: (id: string) => void
  onNavigate: (path: string) => void
}) {
  const Icon = categoryIcons[notification.category] || AlertTriangle
  const navPath = getNavigationPath(notification)

  return (
    <div
      className={cn(
        'group flex gap-3 rounded-md px-3 py-2.5 transition-colors',
        !notification.read && 'bg-accent-dim/30',
        navPath && 'cursor-pointer hover:bg-bg-hover',
      )}
      onClick={() => {
        if (!notification.read) onRead(notification.id)
        if (navPath) onNavigate(navPath)
      }}
    >
      <div className={cn('mt-0.5 shrink-0', severityIconClass(notification.severity))}>
        <Icon className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-2">
          <p className={cn('text-sm leading-tight', !notification.read ? 'font-medium text-text-primary' : 'text-text-secondary')}>
            {notification.title}
          </p>
          {!notification.read && (
            <Button
              variant="ghost"
              size="icon"
              onClick={(e) => {
                e.stopPropagation()
                onRead(notification.id)
              }}
              className="shrink-0 h-6 w-6 text-text-tertiary opacity-0 hover:text-accent group-hover:opacity-100 transition-opacity"
              title="Mark as read"
            >
              <Check className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
        <p className="mt-0.5 text-xs text-text-tertiary line-clamp-2">{notification.message}</p>
        <p className="mt-1 text-[10px] text-text-tertiary">{formatTimestamp(notification.created_at)}</p>
      </div>
    </div>
  )
}

export function NotificationDrawer() {
  const [tab, setTab] = useState<'unread' | 'all'>('unread')
  const { drawerOpen, setDrawerOpen, notifications, unreadCount } = useNotificationStore()
  const navigate = useNavigate()

  useNotificationList(tab)
  useUnreadCount()
  useRealtimeNotifications()

  const markAsRead = useMarkAsRead()
  const markAllAsRead = useMarkAllAsRead()

  const filtered = tab === 'unread' ? notifications.filter((n) => !n.read) : notifications

  const handleRead = (id: string) => {
    markAsRead.mutate(id)
  }

  const handleNavigate = (path: string) => {
    setDrawerOpen(false)
    navigate(path)
  }

  return (
    <Sheet open={drawerOpen} onOpenChange={setDrawerOpen} side="right">
      <SheetHeader>
        <div className="flex items-center justify-between">
          <SheetTitle>Notifications</SheetTitle>
          {unreadCount > 0 && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => markAllAsRead.mutate()}
              className="h-7 gap-1.5 text-xs"
            >
              <CheckCheck className="h-3.5 w-3.5" />
              Mark all as read
            </Button>
          )}
        </div>
      </SheetHeader>

      <Tabs value={tab} onValueChange={(v) => setTab(v as 'unread' | 'all')}>
        <TabsList className="w-full">
          <TabsTrigger value="unread" className="flex-1">
            Unread{unreadCount > 0 && ` (${unreadCount})`}
          </TabsTrigger>
          <TabsTrigger value="all" className="flex-1">
            All
          </TabsTrigger>
        </TabsList>

        <TabsContent value="unread">
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center py-12 text-text-tertiary">
              <Bell className="mb-2 h-8 w-8" />
              <p className="text-sm">No unread notifications</p>
            </div>
          ) : (
            <div className="space-y-1">
              {filtered.map((n) => (
                <NotificationItem key={n.id} notification={n} onRead={handleRead} onNavigate={handleNavigate} />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="all">
          {notifications.length === 0 ? (
            <div className="flex flex-col items-center py-12 text-text-tertiary">
              <Bell className="mb-2 h-8 w-8" />
              <p className="text-sm">No notifications yet</p>
            </div>
          ) : (
            <div className="space-y-1">
              {notifications.map((n) => (
                <NotificationItem key={n.id} notification={n} onRead={handleRead} onNavigate={handleNavigate} />
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </Sheet>
  )
}
