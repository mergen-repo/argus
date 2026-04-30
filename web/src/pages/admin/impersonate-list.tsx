import { useState } from 'react'
import { UserCog, Search, LogIn, Loader2 } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/shared/empty-state'
import { useImpersonation } from '@/hooks/use-impersonation'
import { useUserList } from '@/hooks/use-settings'
import { toast } from 'sonner'

export default function AdminImpersonateListPage() {
  const { data: users = [], isLoading } = useUserList()
  const { impersonate } = useImpersonation()
  const [search, setSearch] = useState('')
  const [impersonating, setImpersonating] = useState<string | null>(null)

  const filtered = users.filter((u) =>
    !search || u.name?.toLowerCase().includes(search.toLowerCase()) || u.email.toLowerCase().includes(search.toLowerCase()),
  )

  const handleImpersonate = async (userId: string, name: string) => {
    setImpersonating(userId)
    try {
      await impersonate.mutateAsync(userId)
      toast.success(`Now impersonating ${name}`)
    } catch {
      // error handled by api interceptor
    } finally {
      setImpersonating(null)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 mb-2">
        <UserCog className="h-5 w-5 text-accent" />
        <h1 className="text-[16px] font-semibold text-text-primary">Impersonate User</h1>
      </div>

      <div className="rounded-[var(--radius-md)] border border-warning/30 bg-warning-dim px-4 py-3 text-sm text-warning">
        <strong>Warning:</strong> All actions taken while impersonating are logged under your account. Only impersonate users with their consent.
      </div>

      <div className="relative max-w-sm">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search by name or email..."
          className="pl-9 h-8 text-sm"
        />
      </div>

      <Card className="overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="w-28" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && Array.from({ length: 5 }).map((_, i) => (
              <TableRow key={i}>
                <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                <TableCell><Skeleton className="h-4 w-40" /></TableCell>
                <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                <TableCell><Skeleton className="h-4 w-24" /></TableCell>
              </TableRow>
            ))}

            {!isLoading && filtered.length === 0 && (
              <TableRow>
                <TableCell colSpan={5}>
                  <EmptyState
                    icon={UserCog}
                    title={search ? 'No users match your search' : 'No users found'}
                  />
                </TableCell>
              </TableRow>
            )}

            {filtered.map((u) => (
              <TableRow key={u.id}>
                <TableCell>
                  <span className="text-sm font-medium text-text-primary">{u.name || '—'}</span>
                </TableCell>
                <TableCell>
                  <span className="text-xs text-text-secondary font-mono">{u.email}</span>
                </TableCell>
                <TableCell>
                  <Badge variant="secondary" className="text-[10px]">{u.role}</Badge>
                </TableCell>
                <TableCell>
                  <Badge variant={u.state === 'active' ? 'success' : 'secondary'} className="text-[10px]">{u.state}</Badge>
                </TableCell>
                <TableCell>
                  <Button
                    variant="outline"
                    size="sm"
                    className="gap-1.5 text-xs"
                    disabled={impersonating === u.id || u.state !== 'active'}
                    onClick={() => handleImpersonate(u.id, u.name || u.email)}
                  >
                    {impersonating === u.id ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <LogIn className="h-3.5 w-3.5" />
                    )}
                    Impersonate
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}
