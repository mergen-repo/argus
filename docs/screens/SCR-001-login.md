# SCR-001: Login

**Type:** Page (full-screen, no sidebar)
**Layout:** AuthLayout — centered card on dark gradient background
**Auth:** None
**Route:** `/login`

## Mockup

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                  │
│                     ◆ ARGUS                                      │
│              APN & Subscriber Intelligence                       │
│                                                                  │
│              ┌──────────────────────────┐                        │
│              │                          │                        │
│              │  Email                   │                        │
│              │  ┌────────────────────┐  │                        │
│              │  │ admin@argus.io     │  │                        │
│              │  └────────────────────┘  │                        │
│              │                          │                        │
│              │  Password                │                        │
│              │  ┌────────────────────┐  │                        │
│              │  │ ••••••••     👁️    │  │                        │
│              │  └────────────────────┘  │                        │
│              │                          │                        │
│              │  [       Sign In       ] │                        │
│              │                          │                        │
│              │  Forgot password?        │                        │
│              └──────────────────────────┘                        │
│                                                                  │
│                                        v1.0.0 — © 2026 Argus    │
└─────────────────────────────────────────────────────────────────┘
```

## States

- **Default:** Empty form
- **Loading:** Button shows spinner, inputs disabled
- **Error:** Red border on invalid field, error text below: "Invalid email or password"
- **Account locked:** "Account locked. Try again in 15 minutes." (after 5 failed attempts)

## Interactions

| Element | Action | Result |
|---------|--------|--------|
| Sign In button | Click | API-001 → success: redirect to / or /setup (if first login). If 2FA enabled → SCR-002 |
| Password eye icon | Toggle | Show/hide password |
| Forgot password | Click | Not in v1 (disabled link) |
| Enter key | Submit | Same as Sign In click |

## API References
- API-001: POST /api/v1/auth/login

## Notes
- Background: subtle animated gradient (dark mode)
- Logo has glow effect (neon accent)
- Card has glass-morphism effect (frosted glass on dark)
