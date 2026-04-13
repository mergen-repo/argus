import { RelatedAuditTab } from '@/components/shared'

interface PolicyAssignmentHistoryTabProps {
  simId: string
}

export function PolicyAssignmentHistoryTab({ simId }: PolicyAssignmentHistoryTabProps) {
  return (
    <div className="mt-4">
      <p className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium mb-3">
        Policy Assignment Events
      </p>
      <RelatedAuditTab
        entityId={simId}
        entityType="sim"
        maxRows={30}
      />
    </div>
  )
}
