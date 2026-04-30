// Minimal, unverified JWT payload reader. The server validates signature and
// expiry — the frontend only needs to read a few claims (tenant_id,
// active_tenant, role) to drive the tenant switcher and System View state.
// DO NOT use this for any trust decision; it's display-only.

export interface TokenPayload {
  sub: string
  tenant_id: string
  role: string
  active_tenant?: string | null
  exp?: number
  iat?: number
}

function base64UrlDecode(input: string): string {
  const pad = input.length % 4 === 0 ? 0 : 4 - (input.length % 4)
  const b64 = (input + '='.repeat(pad)).replace(/-/g, '+').replace(/_/g, '/')
  try {
    return decodeURIComponent(
      atob(b64)
        .split('')
        .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
        .join(''),
    )
  } catch {
    return atob(b64)
  }
}

export function decodeToken(token: string | null | undefined): TokenPayload | null {
  if (!token) return null
  const parts = token.split('.')
  if (parts.length !== 3) return null
  try {
    const json = base64UrlDecode(parts[1])
    const payload = JSON.parse(json) as TokenPayload
    return payload
  } catch {
    return null
  }
}
