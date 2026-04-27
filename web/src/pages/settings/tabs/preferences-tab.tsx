// Tab body for /settings#preferences. Placeholder for future theme/timezone/locale (FIX-240 AC-10).
import { Settings } from 'lucide-react'
import { EmptyState } from '@/components/shared/empty-state'

export default function PreferencesTab() {
  return (
    <EmptyState
      icon={Settings}
      title="Coming soon"
      description="Theme, timezone, and locale preferences will live here in a future release."
    />
  )
}
