import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'

export interface SavedView {
  id: string
  tenant_id: string
  user_id: string
  page: string
  name: string
  filters_json: Record<string, unknown>
  columns_json?: string[]
  sort_json?: { key: string; direction: 'asc' | 'desc' }
  is_default: boolean
  shared: boolean
  created_at: string
  updated_at: string
}

export function useSavedViews(page: string) {
  const qc = useQueryClient()
  const key = ['saved-views', page]

  const query = useQuery({
    queryKey: key,
    queryFn: async () => {
      const res = await api.get<{ status: string; data: SavedView[] }>(
        `/users/me/views?page=${encodeURIComponent(page)}`,
      )
      return res.data.data
    },
    staleTime: 60_000,
  })

  const create = useMutation({
    mutationFn: (body: Omit<SavedView, 'id' | 'tenant_id' | 'user_id' | 'created_at' | 'updated_at'>) =>
      api.post('/users/me/views', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  const update = useMutation({
    mutationFn: ({ id, ...body }: Partial<SavedView> & { id: string }) =>
      api.patch(`/users/me/views/${id}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.delete(`/users/me/views/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  const setDefault = useMutation({
    mutationFn: (id: string) => api.post(`/users/me/views/${id}/default`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  return { ...query, create, update, remove, setDefault }
}
