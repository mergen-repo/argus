export const RAT_DISPLAY: Record<string, string> = {
  nb_iot: 'NB-IoT',
  lte_m: 'LTE-M',
  lte: 'LTE',
  nr_5g: '5G NR',
}

export const RAT_OPTIONS = [
  { value: '', label: 'All RAT' },
  { value: 'nb_iot', label: 'NB-IoT' },
  { value: 'lte_m', label: 'LTE-M' },
  { value: 'lte', label: 'LTE' },
  { value: 'nr_5g', label: '5G NR' },
] as const
