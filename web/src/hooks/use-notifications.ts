import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useCallback } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import { useNotificationStore } from '@/stores/notification'
import type { Notification } from '@/types/notification'
import type { ListResponse, ApiResponse } from '@/types/sim'

const NOTIFICATIONS_KEY = ['notifications'] as const

export function useNotificationList(filter?: 'unread' | 'all') {
  const setNotifications = useNotificationStore((s) => s.setNotifications)

  const query = useQuery({
    queryKey: [...NOTIFICATIONS_KEY, 'list', filter],
    queryFn: async () => {
      const params = new URLSearchParams()
      params.set('limit', '50')
      if (filter === 'unread') params.set('read', 'false')
      const res = await api.get<ListResponse<Notification>>(`/notifications?${params.toString()}`)
      return res.data.data
    },
    staleTime: 15_000,
  })

  useEffect(() => {
    if (query.data) {
      setNotifications(query.data)
    }
  }, [query.data, setNotifications])

  return query
}

export function useUnreadCount() {
  const setUnreadCount = useNotificationStore((s) => s.setUnreadCount)

  const query = useQuery({
    queryKey: [...NOTIFICATIONS_KEY, 'unread-count'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<{ count: number }>>('/notifications/unread-count')
      return res.data.data.count
    },
    staleTime: 30_000,
    refetchInterval: 60_000,
  })

  useEffect(() => {
    if (query.data !== undefined) {
      setUnreadCount(query.data)
    }
  }, [query.data, setUnreadCount])

  return query
}

export function useMarkAsRead() {
  const queryClient = useQueryClient()
  const markAsRead = useNotificationStore((s) => s.markAsRead)

  return useMutation({
    mutationFn: async (id: string) => {
      await api.patch(`/notifications/${id}/read`)
      return id
    },
    onMutate: (id) => {
      markAsRead(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: NOTIFICATIONS_KEY })
    },
  })
}

export function useMarkAllAsRead() {
  const queryClient = useQueryClient()
  const markAllAsRead = useNotificationStore((s) => s.markAllAsRead)

  return useMutation({
    mutationFn: async () => {
      await api.post('/notifications/mark-all-read')
    },
    onMutate: () => {
      markAllAsRead()
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: NOTIFICATIONS_KEY })
    },
  })
}

export function useRealtimeNotifications() {
  const addNotification = useNotificationStore((s) => s.addNotification)
  const queryClient = useQueryClient()

  const handler = useCallback(
    (data: unknown) => {
      const notification = data as Notification
      if (notification.id) {
        addNotification(notification)
        queryClient.invalidateQueries({ queryKey: [...NOTIFICATIONS_KEY, 'unread-count'] })
      }
    },
    [addNotification, queryClient],
  )

  useEffect(() => {
    const unsub = wsClient.on('notification.new', handler)
    return unsub
  }, [handler])
}
