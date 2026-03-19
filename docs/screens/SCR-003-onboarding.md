# SCR-003: Onboarding Wizard

**Type:** Page (full-screen, minimal chrome)
**Layout:** Wizard layout — progress bar + step content + nav buttons
**Auth:** JWT (tenant_admin, first login only)
**Route:** `/setup`

## Step 1: Welcome

```
┌─────────────────────────────────────────────────────────────────┐
│  ◆ ARGUS                                         Step 1 of 5    │
├─────────────────────────────────────────────────────────────────┤
│  ●───○───○───○───○                                               │
│  Welcome  Operators  APNs  Import  Policies                     │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                                                              ││
│  │  Welcome to Argus, Bora! 👋                                  ││
│  │                                                              ││
│  │  Let's set up your environment in 5 quick steps.            ││
│  │                                                              ││
│  │  Your Tenant: Acme Energy Corp                               ││
│  │  Role: Tenant Admin                                          ││
│  │                                                              ││
│  │  What we'll do:                                              ││
│  │  1. Connect your mobile operators                            ││
│  │  2. Define your APNs (Access Point Names)                    ││
│  │  3. Import your first batch of SIMs                          ││
│  │  4. Set default policies                                     ││
│  │  5. Configure notifications                                  ││
│  │                                                              ││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                  │
│                                           [Get Started →]        │
└─────────────────────────────────────────────────────────────────┘
```

## Step 2: Connect Operators

```
┌─────────────────────────────────────────────────────────────────┐
│  ◆ ARGUS                                         Step 2 of 5    │
├─────────────────────────────────────────────────────────────────┤
│  ●───●───○───○───○                                               │
│  Welcome  Operators  APNs  Import  Policies                     │
│                                                                  │
│  Connect Your Operators                                          │
│  Select operators your tenant has access to:                     │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ ☑ Turkcell        MCC:286 MNC:01   🟢 Connected        │   │
│  │ ☑ Vodafone        MCC:286 MNC:02   🟡 Pending          │   │
│  │ ☐ TT Mobile       MCC:286 MNC:03   ○ Not connected     │   │
│  │ ☑ Mock Simulator   MCC:001 MNC:01   🟢 Connected (Dev) │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ℹ️ Operators are managed by your system administrator.          │
│     Contact admin to add new operator connections.               │
│                                                                  │
│                              [← Previous]  [Next: Define APNs →]│
└─────────────────────────────────────────────────────────────────┘
```

## Step 3: Define APNs

```
┌─────────────────────────────────────────────────────────────────┐
│  ◆ ARGUS                                         Step 3 of 5    │
├─────────────────────────────────────────────────────────────────┤
│  ●───●───●───○───○                                               │
│  Welcome  Operators  APNs  Import  Policies                     │
│                                                                  │
│  Define Your APNs                                  [+ Add APN]  │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ APN Name     │ Operator  │ Type             │ RAT Types  │   │
│  ├──────────────┼───────────┼──────────────────┼────────────┤   │
│  │ iot.fleet    │ Turkcell  │ Private Managed  │ LTE-M, LTE │   │
│  │ iot.meter    │ Turkcell  │ Private Managed  │ NB-IoT     │   │
│  │ iot.test     │ Mock Sim  │ Private Managed  │ All        │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  You can add more APNs later from the APN Management page.     │
│                                                                  │
│                          [← Previous]  [Next: Import SIMs →]    │
└─────────────────────────────────────────────────────────────────┘
```

## Step 4: Import SIMs

```
┌─────────────────────────────────────────────────────────────────┐
│  ◆ ARGUS                                         Step 4 of 5    │
├─────────────────────────────────────────────────────────────────┤
│  ●───●───●───●───○                                               │
│  Welcome  Operators  APNs  Import  Policies                     │
│                                                                  │
│  Import Your First SIMs                                          │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                                                          │   │
│  │     ⬆️  Drag & drop CSV file here                        │   │
│  │         or click to browse                               │   │
│  │                                                          │   │
│  │     Required columns: ICCID, IMSI, MSISDN,              │   │
│  │     Operator, APN                                        │   │
│  │                                                          │   │
│  │     📥 Download CSV template                              │   │
│  │                                                          │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Or skip this step and import SIMs later.                       │
│                                                                  │
│                       [← Previous]  [Skip]  [Next: Policies →]  │
└─────────────────────────────────────────────────────────────────┘
```

## Step 5: Set Policies

```
┌─────────────────────────────────────────────────────────────────┐
│  ◆ ARGUS                                         Step 5 of 5    │
├─────────────────────────────────────────────────────────────────┤
│  ●───●───●───●───●                                               │
│  Welcome  Operators  APNs  Import  Policies                     │
│                                                                  │
│  Set Default Policies                                            │
│                                                                  │
│  Assign a default policy to each APN:                           │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ APN          │ Default Policy                    │       │   │
│  ├──────────────┼───────────────────────────────────┼───────┤   │
│  │ iot.fleet    │ [Standard IoT ▼]                  │ Edit  │   │
│  │ iot.meter    │ [Low Bandwidth NB-IoT ▼]          │ Edit  │   │
│  │ iot.test     │ [Unlimited (Dev) ▼]               │ Edit  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  You can create custom policies later from the Policy Editor.   │
│                                                                  │
│                          [← Previous]  [Complete Setup ✓]        │
└─────────────────────────────────────────────────────────────────┘
```

## States

- **Progress:** Steps turn filled (●) as completed
- **Skip:** Steps 4-5 can be skipped
- **Error:** Inline validation per step
- **Complete:** Redirect to SCR-010 (Dashboard) with welcome toast

## API References
- Step 2: API-025 (list grants)
- Step 3: API-031 (create APN)
- Step 4: API-063 (bulk import)
- Step 5: API-091, API-095 (create + activate policy)
