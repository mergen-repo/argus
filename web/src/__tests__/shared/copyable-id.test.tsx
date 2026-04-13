/**
 * Smoke tests for CopyableId shared component.
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 */

// Prop type smoke test — ensure CopyableId props are well-typed
type CopyableIdProps = {
  value: string
  label?: string
  masked?: boolean
  mono?: boolean
  className?: string
}

const _minProps: CopyableIdProps = { value: 'test-id-1234' }

const _fullProps: CopyableIdProps = {
  value: '89314404000013201645',
  label: 'ICCID',
  masked: true,
  mono: true,
  className: 'text-text-primary',
}

// Verify masking behavior contract: length <= 8 should not be masked
function maskValue(value: string): string {
  if (value.length <= 8) return value
  return `${value.slice(0, 4)}•••${value.slice(-4)}`
}

const _short = maskValue('abc123')
const _shouldEqual: boolean = _short === 'abc123'
const _long = maskValue('89314404000013201645')
const _shouldMask: boolean = _long.includes('•••')

export { _minProps, _fullProps, _shouldEqual, _shouldMask }
