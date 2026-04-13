import { useState } from 'react'
import { MessageSquare, Send, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { useAnomalyComments, useAddAnomalyComment } from '@/hooks/use-ops'
import { cn } from '@/lib/utils'

interface CommentThreadProps {
  anomalyId: string
  open: boolean
  onClose: () => void
}

function timeLabel(ts: string): string {
  const d = new Date(ts)
  const now = Date.now()
  const diff = now - d.getTime()
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return d.toLocaleDateString()
}

export function CommentThread({ anomalyId, open, onClose }: CommentThreadProps) {
  const [draft, setDraft] = useState('')
  const { data: comments, isLoading } = useAnomalyComments(anomalyId)
  const addComment = useAddAnomalyComment(anomalyId)

  const handleSend = async () => {
    const body = draft.trim()
    if (!body) return
    await addComment.mutateAsync(body)
    setDraft('')
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      handleSend()
    }
  }

  if (!open) return null

  return (
    <div
      className={cn(
        'fixed inset-y-0 right-0 z-50 flex flex-col w-full max-w-sm',
        'bg-bg-surface border-l border-border shadow-2xl',
        'animate-slide-up-in',
      )}
    >
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2">
          <MessageSquare className="h-4 w-4 text-accent" />
          <span className="text-[13px] font-semibold text-text-primary">Investigation Thread</span>
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={onClose}
          className="h-7 w-7 p-0 text-text-tertiary hover:text-text-primary"
          aria-label="Close thread"
        >
          <X className="h-4 w-4" />
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
        {isLoading ? (
          <>
            <Skeleton className="h-16" />
            <Skeleton className="h-12" />
            <Skeleton className="h-20" />
          </>
        ) : !comments || comments.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full gap-2 py-12 text-center">
            <MessageSquare className="h-8 w-8 text-text-tertiary" />
            <p className="text-[13px] text-text-secondary">No comments yet</p>
            <p className="text-[11px] text-text-tertiary">Add investigation notes below</p>
          </div>
        ) : (
          comments.map((c) => (
            <div
              key={c.id}
              className="rounded-[10px] bg-bg-elevated border border-border px-3 py-2.5 space-y-1"
            >
              <div className="flex items-center justify-between">
                <span className="text-[11px] font-medium text-accent">{c.user_email}</span>
                <span className="text-[10px] text-text-tertiary font-mono">{timeLabel(c.created_at)}</span>
              </div>
              <p className="text-[13px] text-text-primary whitespace-pre-wrap break-words">{c.body}</p>
            </div>
          ))
        )}
      </div>

      <div className="px-4 py-3 border-t border-border space-y-2">
        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value.slice(0, 2000))}
          onKeyDown={handleKeyDown}
          placeholder="Add a note… (⌘Enter to send)"
          className="bg-bg-elevated border-border text-text-primary placeholder:text-text-tertiary resize-none text-[13px]"
          rows={3}
        />
        <div className="flex items-center justify-between">
          <span className="text-[10px] text-text-tertiary">{draft.length}/2000</span>
          <Button
            size="sm"
            onClick={handleSend}
            disabled={!draft.trim() || addComment.isPending}
            className="bg-accent text-bg-primary hover:bg-accent/90 h-7 px-3 gap-1.5 text-[12px]"
          >
            {addComment.isPending ? <Spinner className="h-3 w-3" /> : <Send className="h-3 w-3" />}
            Send
          </Button>
        </div>
      </div>
    </div>
  )
}
