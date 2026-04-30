import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'
import type { TenantUsageItem } from '@/types/admin'

const TENANT_USAGE_KEY = ['admin', 'tenant-usage'] as const

export function useTenantUsage() {
  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: TENANT_USAGE_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<TenantUsageItem[]>>('/admin/tenants/usage')
      return res.data.data ?? []
    },
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  return { data: data ?? [], isLoading, isError, error, refetch }
}
