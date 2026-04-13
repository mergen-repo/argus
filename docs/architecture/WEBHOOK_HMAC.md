# Webhook HMAC Signature Verification

## Overview

Every outbound webhook request from Argus is signed with HMAC-SHA256 using the shared secret configured per webhook endpoint. Receivers must verify this signature before processing the payload to ensure authenticity and integrity.

## Headers Sent by Argus

| Header | Example Value | Description |
|--------|--------------|-------------|
| `X-Argus-Signature` | `sha256=a3f2...` | HMAC-SHA256 of the raw request body, hex-encoded, prefixed with `sha256=` |
| `X-Argus-Event` | `sim.activated` | The event type that triggered this delivery |
| `X-Argus-Timestamp` | `2026-04-13T12:00:00Z` | UTC timestamp when the request was sent (RFC 3339) |
| `Content-Type` | `application/json` | Always `application/json` |
| `User-Agent` | `Argus-Webhook/1.0` | Fixed user agent string |

## Signature Computation

```
signature = HMAC-SHA256(secret, raw_body_bytes)
header    = "sha256=" + hex.encode(signature)
```

The HMAC key is the UTF-8 encoding of the webhook secret you provided when creating the config.

## Verification Examples

### Node.js

```js
const crypto = require('crypto');

function verifyArgusSignature(secret, rawBody, headerValue) {
  const expected = 'sha256=' + crypto
    .createHmac('sha256', secret)
    .update(rawBody)           // rawBody must be Buffer or string (utf-8 bytes)
    .digest('hex');
  return crypto.timingSafeEqual(
    Buffer.from(expected, 'utf8'),
    Buffer.from(headerValue, 'utf8')
  );
}

// Express example — use express.raw() or similar to get the raw body
app.post('/webhook', express.raw({ type: 'application/json' }), (req, res) => {
  const sig = req.headers['x-argus-signature'];
  if (!verifyArgusSignature(process.env.WEBHOOK_SECRET, req.body, sig)) {
    return res.status(401).send('Invalid signature');
  }
  const event = JSON.parse(req.body);
  // process event...
  res.sendStatus(200);
});
```

### Python

```python
import hashlib
import hmac

def verify_argus_signature(secret: str, raw_body: bytes, header_value: str) -> bool:
    expected = 'sha256=' + hmac.new(
        secret.encode('utf-8'),
        raw_body,
        hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, header_value)

# Flask example
from flask import Flask, request, abort

app = Flask(__name__)

@app.route('/webhook', methods=['POST'])
def webhook():
    sig = request.headers.get('X-Argus-Signature', '')
    if not verify_argus_signature(WEBHOOK_SECRET, request.get_data(), sig):
        abort(401)
    event = request.get_json()
    # process event...
    return '', 200
```

### Go

```go
package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/hex"
    "io"
    "net/http"
)

func verifyArgusSignature(secret string, rawBody []byte, headerValue string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(rawBody)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    return subtle.ConstantTimeCompare([]byte(expected), []byte(headerValue)) == 1
}

func webhookHandler(secret string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
        if err != nil {
            http.Error(w, "read error", http.StatusBadRequest)
            return
        }
        sig := r.Header.Get("X-Argus-Signature")
        if !verifyArgusSignature(secret, body, sig) {
            http.Error(w, "invalid signature", http.StatusUnauthorized)
            return
        }
        // process body (JSON)...
        w.WriteHeader(http.StatusOK)
    }
}
```

## Security Notes

1. **Always use constant-time comparison** — comparing signatures with `==` or `string.Compare` is vulnerable to timing attacks. Use `hmac.Equal`, `crypto/subtle.ConstantTimeCompare`, or your language's equivalent (`hmac.compare_digest` in Python, `crypto.timingSafeEqual` in Node).

2. **Read the raw body before parsing** — the HMAC is computed over the raw bytes. If you parse JSON first and re-serialize, byte order or whitespace may differ and verification will fail. Always read the body once, verify, then parse.

3. **Timestamp skew (optional but recommended)** — Argus sends `X-Argus-Timestamp` but does not currently enforce clock skew on the sender side. Receivers are encouraged to reject requests where the timestamp is more than 5 minutes old or in the future to prevent replay attacks:

   ```python
   from datetime import datetime, timezone, timedelta

   ts = datetime.fromisoformat(request.headers['X-Argus-Timestamp'])
   if abs((datetime.now(timezone.utc) - ts)) > timedelta(minutes=5):
       abort(400, 'Timestamp too skewed')
   ```

4. **HTTPS only** — Argus refuses to dispatch webhooks to `http://` endpoints. Always use `https://` URLs.

5. **Secret rotation** — when you rotate the webhook secret via `PATCH /api/v1/webhooks/:id`, new deliveries immediately use the new secret. In-flight retries for deliveries signed with the old secret will fail verification on your end. Schedule rotation during a low-traffic window if possible.
