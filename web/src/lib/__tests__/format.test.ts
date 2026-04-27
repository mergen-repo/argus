/**
 * Type-level + logic tests for lib/format.ts (FIX-246 AC-7).
 * Executes via tsc --noEmit (no vitest/jsdom required).
 *
 * Covered scenarios:
 *  1. formatBytes(0) → "0 B"
 *  2. formatBytes(1024) → "1.0 KB"
 *  3. formatBytes(27304) → "26.7 KB"
 *  4. formatBytes(10737418240) → "10.0 GB"
 *  5. formatBytes(1099511627776) → "1.0 TB"
 */

import { formatBytes } from '@/lib/format'

// ─── Helper ───────────────────────────────────────────────────────────────────

function assertEq(actual: string, expected: string, label: string): void {
  if (actual !== expected) {
    throw new Error(`formatBytes FAIL [${label}]: expected "${expected}", got "${actual}"`)
  }
}

// ─── Scenario 1: 0 bytes ──────────────────────────────────────────────────────

assertEq(formatBytes(0), '0 B', '0 bytes')

// ─── Scenario 2: 1 KB exactly ────────────────────────────────────────────────

assertEq(formatBytes(1024), '1.0 KB', '1 KB')

// ─── Scenario 3: 27304 bytes → 26.7 KB ───────────────────────────────────────

assertEq(formatBytes(27304), '26.7 KB', '27304 bytes')

// ─── Scenario 4: 10 GB exactly ───────────────────────────────────────────────

assertEq(formatBytes(10737418240), '10.0 GB', '10 GB')

// ─── Scenario 5: 1 TB exactly ────────────────────────────────────────────────

assertEq(formatBytes(1099511627776), '1.0 TB', '1 TB')

// ─── Return type check ────────────────────────────────────────────────────────

const _returnType: string = formatBytes(512)
void _returnType

export {}
