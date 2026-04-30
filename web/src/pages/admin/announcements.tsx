import { useState } from 'react'
import { Plus, Trash2, Edit2, Megaphone, Loader2, X } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/shared/empty-state'
import { useAdminAnnouncements, type Announcement } from '@/hooks/use-announcements'
import { timeAgo } from '@/lib/format'

const TYPE_OPTIONS = [
  { value: 'info', label: 'Info' },
  { value: 'warning', label: 'Warning' },
  { value: 'critical', label: 'Critical' },
]

type AnnouncementType = 'info' | 'warning' | 'critical'

const typeVariant: Record<AnnouncementType, 'default' | 'warning' | 'danger'> = {
  info: 'default',
  warning: 'warning',
  critical: 'danger',
}

export default function AdminAnnouncementsPage() {
  const { data: announcements = [], isLoading, create, update, remove } = useAdminAnnouncements()
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState({ title: '', body: '', type: 'info' as AnnouncementType, ends_at: '', target: 'all', starts_at: '', dismissible: true })
  const [submitting, setSubmitting] = useState(false)

  const resetForm = () => {
    setForm({ title: '', body: '', type: 'info', ends_at: '', target: 'all', starts_at: '', dismissible: true })
    setEditing(null)
    setShowCreate(false)
  }

  const handleSubmit = async () => {
    if (!form.title.trim() || !form.body.trim()) return
    setSubmitting(true)
    try {
      if (editing) {
        await update.mutateAsync({ id: editing, ...form })
      } else {
        await create.mutateAsync(form)
      }
      resetForm()
    } finally {
      setSubmitting(false)
    }
  }

  const startEdit = (a: Announcement) => {
    setEditing(a.id)
    setForm({
      title: a.title,
      body: a.body,
      type: a.type,
      ends_at: a.ends_at ?? '',
      target: a.target ?? 'all',
      starts_at: a.starts_at ?? '',
      dismissible: a.dismissible ?? true,
    })
    setShowCreate(true)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <Megaphone className="h-5 w-5 text-accent" />
          <h1 className="text-[16px] font-semibold text-text-primary">Announcements</h1>
        </div>
        <Button size="sm" className="gap-2" onClick={() => { resetForm(); setShowCreate(true) }}>
          <Plus className="h-4 w-4" />
          New Announcement
        </Button>
      </div>

      {isLoading && (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i} className="p-4 space-y-2">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-3 w-full" />
            </Card>
          ))}
        </div>
      )}

      {!isLoading && announcements.length === 0 && (
        <EmptyState
          icon={Megaphone}
          title="No announcements"
          description="Create an announcement to notify all users."
          ctaLabel="New Announcement"
          onCta={() => { resetForm(); setShowCreate(true) }}
        />
      )}

      {!isLoading && announcements.length > 0 && (
        <div className="space-y-2">
          {announcements.map((a) => (
            <Card key={a.id} className="p-4 flex items-start justify-between gap-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 mb-1">
                  <Badge variant={typeVariant[(a.type as AnnouncementType)] ?? 'default'} className="text-[10px]">
                    {a.type}
                  </Badge>
                  <span className="text-sm font-medium text-text-primary truncate">{a.title}</span>
                </div>
                <p className="text-xs text-text-secondary">{a.body}</p>
                {a.ends_at && (
                  <p className="text-[10px] text-text-tertiary mt-1">Expires: {timeAgo(a.ends_at)}</p>
                )}
              </div>
              <div className="flex items-center gap-1 shrink-0">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 text-text-tertiary hover:text-text-primary"
                  onClick={() => startEdit(a)}
                >
                  <Edit2 className="h-3.5 w-3.5" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 text-text-tertiary hover:text-danger"
                  onClick={() => remove.mutate(a.id)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={showCreate} onOpenChange={(v) => { if (!v) resetForm() }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit Announcement' : 'New Announcement'}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1">Title *</label>
              <Input
                placeholder="e.g. Scheduled maintenance on Friday"
                value={form.title}
                onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1">Message *</label>
              <Textarea
                value={form.body}
                onChange={(e) => setForm((f) => ({ ...f, body: e.target.value }))}
                placeholder="Describe the announcement..."
                className="h-24 resize-none"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1">Type</label>
              <Select
                value={form.type}
                onChange={(e) => setForm((f) => ({ ...f, type: e.target.value as AnnouncementType }))}
                options={TYPE_OPTIONS}
                className="h-8 text-sm"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1">Ends At (optional)</label>
              <Input
                type="datetime-local"
                value={form.ends_at}
                onChange={(e) => setForm((f) => ({ ...f, ends_at: e.target.value }))}
                className="h-8 text-sm"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={resetForm}>Cancel</Button>
            <Button size="sm" onClick={handleSubmit} disabled={submitting || !form.title.trim() || !form.body.trim()}>
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : (editing ? 'Update' : 'Create')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
