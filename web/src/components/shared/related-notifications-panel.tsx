import * as React from 'react'
import { Link } from 'react-router-dom'
import { Bell, Mail, Webhook, MessageSquare, ArrowRight } from 'lucide-react'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from './empty-state'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ListResponse } from '@/types/sim'
import type { Notification } from '@/types/notification'
import { timeAgo } from '@/lib/format'

interface RelatedNotificationsPanelProps {
  entityId: string
  limit?: number
}

function channelIcon(type: string) {
  if (type?.includes('email')) return <Mail className="h-3.5 w-3.5 text-text-tertiary flex-shrink-0" />
  if (type?.includes('webhook')) return <Webhook className="h-3.5 w-3.5 text-text-tertiary flex-shrink-0" />
  if (type?.includes('telegram') || type?.includes('sms')) return <MessageSquare className="h-3.5 w-3.5 text-text-tertiary flex-shrink-0" />
  return <Bell className="h-3.5 w-3.5 text-text-tertiary flex-shrink-0" />
}

function useRelatedNotifications(resourceId: string, limit: number) {
  return useQuery({
    queryKey: ['notifications', 'related', resourceId, limit],
    queryFn: async () => {
      const params = new URLSearchParams({ resource_id: resourceId, limit: String(limit) })
      const res = await api.get<ListResponse<Notification>>(`/notifications?${params.toString()}`)
      return res.data
    },
    staleTime: 30_000,
    enabled: !!resourceId,
  })
}

export function RelatedNotificationsPanel({
  entityId,
  limit = 5,
}: RelatedNotificationsPanelProps) {
  const { data, isLoading, isError } = useRelatedNotifications(entityId, limit)
  const notifications = data?.data ?? []
  const count = notifications.length

  return (
    <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
      <CardHeader className="py-3 px-4 border-b border-border-subtle flex flex-row items-center justify-between space-y-0">
        <div className="flex items-center gap-2">
          <Bell className="h-4 w-4 text-text-tertiary" />
          <span className="text-[13px] font-medium text-text-primary">Notifications</span>
          {count > 0 && (
            <Badge variant="secondary" className="text-[10px] h-4 px-1.5">
              {count}
            </Badge>
          )}
        </div>
        <Link
          to={`/notifications?resource_id=${entityId}`}
          className="inline-flex items-center gap-1 text-[11px] text-accent hover:text-accent/80 transition-colors duration-200"
        >
          View all <ArrowRight className="h-3 w-3" />
        </Link>
      </CardHeader>
      <CardContent className="p-0">
        {isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : isError ? (
          <div className="py-6 text-center">
            <p className="text-[13px] text-danger">Failed to load notifications</p>
          </div>
        ) : notifications.length === 0 ? (
          <EmptyState
            icon={Bell}
            title="No notifications"
            description="Notifications for this entity will appear here."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="border-b border-border-subtle hover:bg-transparent">
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Channel</TableHead>
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Title</TableHead>
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Severity</TableHead>
                <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Time</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {notifications.map((notif) => (
                <TableRow key={notif.id} className="border-b border-border-subtle hover:bg-bg-hover transition-colors">
                  <TableCell className="py-2">
                    <span className="flex items-center gap-1.5">
                      {channelIcon(notif.type)}
                      <span className="text-[11px] text-text-secondary capitalize">{notif.type}</span>
                    </span>
                  </TableCell>
                  <TableCell className="py-2">
                    <span className="text-[12px] text-text-primary truncate max-w-[200px] block">{notif.title}</span>
                  </TableCell>
                  <TableCell className="py-2">
                    <SeverityBadge severity={notif.severity} />
                  </TableCell>
                  <TableCell className="py-2">
                    <span className="text-[11px] text-text-tertiary">{timeAgo(notif.created_at)}</span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
