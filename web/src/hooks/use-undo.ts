import { useRef, useEffect, useCallback, useState } from 'react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useQueryClient } from '@tanstack/react-query'

const UNDO_COUNTDOWN_SEC = 10

export function useUndo(invalidateKeys?: string[][]) {
  const qc = useQueryClient()
  const toastIdRef = useRef<string | number | null>(null)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const clearPending = useCallback(() => {
    if (timerRef.current) clearInterval(timerRef.current)
    if (toastIdRef.current) toast.dismiss(toastIdRef.current)
    abortRef.current?.abort()
    timerRef.current = null
    toastIdRef.current = null
  }, [])

  useEffect(() => () => clearPending(), [clearPending])

  const register = useCallback(
    (actionId: string, message: string) => {
      clearPending()
      let remaining = UNDO_COUNTDOWN_SEC
      abortRef.current = new AbortController()

      const doUndo = async () => {
        clearPending()
        try {
          await api.post(`/undo/${actionId}`, {}, { signal: abortRef.current?.signal })
          toast.success('Action undone')
          invalidateKeys?.forEach((k) => qc.invalidateQueries({ queryKey: k }))
        } catch {
          toast.error('Undo expired or failed')
        }
      }

      toastIdRef.current = toast(message, {
        duration: UNDO_COUNTDOWN_SEC * 1000,
        action: { label: `Undo (${remaining}s)`, onClick: doUndo },
      })

      timerRef.current = setInterval(() => {
        remaining--
        if (remaining <= 0) {
          clearPending()
          return
        }
        if (toastIdRef.current != null) {
          toast(message, {
            id: toastIdRef.current,
            duration: remaining * 1000,
            action: { label: `Undo (${remaining}s)`, onClick: doUndo },
          })
        }
      }, 1000)
    },
    [clearPending, invalidateKeys, qc],
  )

  return { register, clearPending }
}

export function useUndoState() {
  const [actionId, setActionId] = useState<string | null>(null)
  const { register, clearPending } = useUndo()

  const trigger = useCallback(
    (id: string, message: string, invalidateKeys?: string[][]) => {
      setActionId(id)
      register(id, message)
    },
    [register],
  )

  return { actionId, trigger, clearPending }
}
