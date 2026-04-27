// FIX-244 DEV-528: this module is now a thin re-export of use-violations.ts
// for backwards compatibility with existing detail-page imports. New code
// should import directly from '@/hooks/use-violations'.

export { useViolationDetail as useViolation, useRemediate } from './use-violations'
