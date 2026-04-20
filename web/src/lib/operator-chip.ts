export type OperatorCode = 'turkcell' | 'vodafone_tr' | 'turk_telekom' | (string & {}) | null | undefined;

export interface OperatorChipColor {
  dot: string;
  bg: string;
}

export function operatorChipColor(code: OperatorCode): OperatorChipColor {
  switch (code) {
    case 'turkcell':
      return { dot: 'bg-warning', bg: 'bg-warning-dim' };
    case 'vodafone_tr':
      return { dot: 'bg-danger', bg: 'bg-danger-dim' };
    case 'turk_telekom':
      return { dot: 'bg-info', bg: 'bg-info-dim' };
    default:
      return { dot: 'bg-text-tertiary', bg: 'bg-bg-elevated' };
  }
}
