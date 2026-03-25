import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useUIStore } from '@/stores/ui'

const NAV_MAP: Record<string, string> = {
  d: '/',
  s: '/sims',
  o: '/operators',
  p: '/policies',
  a: '/alerts',
  n: '/sessions',
  j: '/jobs',
  l: '/audit',
  t: '/topology',
  r: '/reports',
  c: '/capacity',
}

export function useKeyboardNav() {
  const navigate = useNavigate()
  const { toggleSidebar } = useUIStore()
  const waitingForNav = useRef(false)
  const timeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement
      if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) return

      if (e.key === 'g' && !e.ctrlKey && !e.metaKey) {
        waitingForNav.current = true
        clearTimeout(timeoutRef.current)
        timeoutRef.current = setTimeout(() => {
          waitingForNav.current = false
        }, 1000)
        return
      }

      if (waitingForNav.current) {
        waitingForNav.current = false
        clearTimeout(timeoutRef.current)
        const path = NAV_MAP[e.key]
        if (path) {
          e.preventDefault()
          navigate(path)
        }
        return
      }

      if (e.key === '[' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        toggleSidebar()
      }
      if (e.key === ']' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        toggleSidebar()
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      clearTimeout(timeoutRef.current)
    }
  }, [navigate, toggleSidebar])
}
