import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'

export interface Announcement {
  id: string
  title: string
  body: string
  type: 'info' | 'warning' | 'critical'
  target: string
  starts_at: string
  ends_at: string
  dismissible: boolean
  created_by?: string
  created_at: string
}

const ACTIVE_KEY = ['announcements', 'active']
const LOCAL_DISMISSED = 'argus:dismissed-announcements'

function getDismissedIds(): string[] {
  try {
    return JSON.parse(localStorage.getItem(LOCAL_DISMISSED) || '[]')
  } catch {
    return []
  }
}

function addDismissedId(id: string) {
  const ids = getDismissedIds()
  if (!ids.includes(id)) {
    localStorage.setItem(LOCAL_DISMISSED, JSON.stringify([...ids, id]))
  }
}

export function useAnnouncements() {
  const qc = useQueryClient()

  const query = useQuery({
    queryKey: ACTIVE_KEY,
    queryFn: async () => {
      const res = await api.get<{ status: string; data: Announcement[] }>('/announcements/active')
      const dismissed = getDismissedIds()
      return res.data.data.filter((a) => !dismissed.includes(a.id))
    },
    staleTime: 60_000,
  })

  const dismiss = useMutation({
    mutationFn: async (id: string) => {
      addDismissedId(id)
      qc.setQueryData<Announcement[]>(ACTIVE_KEY, (prev) =>
        prev ? prev.filter((a) => a.id !== id) : [],
      )
      await api.post(`/announcements/${id}/dismiss`, {})
    },
    onError: (_err, id) => {
      qc.invalidateQueries({ queryKey: ACTIVE_KEY })
    },
  })

  return { ...query, dismiss }
}

export function useAdminAnnouncements() {
  const qc = useQueryClient()
  const key = ['announcements', 'admin']

  const query = useQuery({
    queryKey: key,
    queryFn: async () => {
      const res = await api.get<{ status: string; data: Announcement[] }>('/announcements')
      return res.data.data
    },
  })

  const create = useMutation({
    mutationFn: (body: Omit<Announcement, 'id' | 'created_at'>) =>
      api.post('/announcements', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  const update = useMutation({
    mutationFn: ({ id, ...body }: Partial<Announcement> & { id: string }) =>
      api.patch(`/announcements/${id}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.delete(`/announcements/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  return { ...query, create, update, remove }
}
