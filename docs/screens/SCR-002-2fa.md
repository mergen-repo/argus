# SCR-002: Two-Factor Authentication

**Type:** Page (full-screen, no sidebar)
**Layout:** AuthLayout
**Auth:** Partial (post-password, pre-session)
**Route:** `/login/2fa`

## Mockup

```
┌─────────────────────────────────────────────────────────────────┐
│                          ◆ ARGUS                                 │
│                                                                  │
│              ┌──────────────────────────┐                        │
│              │                          │                        │
│              │  Two-Factor Auth         │                        │
│              │                          │                        │
│              │  Enter the 6-digit code  │                        │
│              │  from your authenticator  │                        │
│              │                          │                        │
│              │  ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐                 │
│              │  │ 4│ │ 2│ │ 8│ │  │ │  │ │  │                 │
│              │  └──┘ └──┘ └──┘ └──┘ └──┘ └──┘                 │
│              │                          │                        │
│              │  [      Verify         ] │                        │
│              │                          │                        │
│              │  ← Back to login         │                        │
│              └──────────────────────────┘                        │
└─────────────────────────────────────────────────────────────────┘
```

## States

- **Default:** Empty code inputs, auto-focus first box
- **Typing:** Auto-advance to next box on digit entry
- **Loading:** Verify button shows spinner
- **Error:** Boxes shake + red border, "Invalid code. Try again."
- **Too many attempts:** "Too many failed attempts. Please wait 5 minutes."

## Interactions

| Element | Action | Result |
|---------|--------|--------|
| Code inputs | Type digit | Auto-advance, auto-submit on 6th digit |
| Verify button | Click | API-005 → success: redirect to / |
| Back to login | Click | Return to SCR-001, clear session |
| Paste | Paste 6 digits | Auto-fill all boxes, auto-submit |

## API References
- API-005: POST /api/v1/auth/2fa/verify
