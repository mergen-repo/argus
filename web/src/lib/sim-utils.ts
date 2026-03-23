import type { SIMState } from '@/types/sim'

export function stateVariant(state: SIMState): 'success' | 'warning' | 'danger' | 'default' | 'secondary' {
  switch (state) {
    case 'active': return 'success'
    case 'suspended': return 'warning'
    case 'terminated': return 'danger'
    case 'stolen_lost': return 'danger'
    case 'ordered': return 'default'
    default: return 'secondary'
  }
}

export function stateLabel(state: string): string {
  switch (state) {
    case 'stolen_lost': return 'LOST/STOLEN'
    default: return state.toUpperCase()
  }
}
