# Authentication

Below is a thorough explanation of how authentication and authorization work in the application, centered on **JWT** and how it is wired through the HTTP API, the alarm manager, and the rest of the system.

---

## Big picture

**Client** (sends HTTP request with Bearer JWT)
         |
         ▼
**API middleware** (withAuth):
  - If JWT enabled: parse & verify token → claims in request context
  - If JWT disabled: inject admin claims (dev mode)
         |
         ▼
**Endpoint handlers** (rules & alarms):
  - Check role requirement (viewer|operator|admin)
  - Use `claims.Sub` as 'actor' for audit events
         |
         ▼
**Business logic** (manager, repos):
  - Executes op (e.g. ack/silence/clear), persists events with actor

- The JWT is a symmetrically signed token (**HS256**) that carries minimal claims:
    - subject (sub)
    - role (role)
    - optional expiration (exp)
- The Authorization header must be Bearer `<token>`.
- If `JWT_SECRET` is not set, the API runs in **dev mode** (no verification; every call treated as admin).
- Role checks gate access to endpoints in precedence order:
    - viewer
    - operator
    - admin
- The actor used in audit events (e.g. ack, silence) is `claims.Sub`.

---

## Token format and generation
- Token is standard JWT:
    - **Header:** `{ "alg": "HS256", "typ": "JWT" }`
    - **Payload (claims):** `{"sub": "...", "role": "viewer|operator|admin", "exp": <unix-seconds>}`
    - **Signature:** HMAC‑SHA256 over `base64url(header) + "." + base64url(payload)`
- You generate tokens with the included helper:

```bash
export JWT_SECRET="your-strong-secret"
go run ./cmd/jwtgen -sub ops@org_name -role operator -secret "$JWT_SECRET" -exp 1h
```

That prints a compact JWT string you pass in the Authorization header.

---

## Verification

File: `/internal/auth/jwt.go`

### Claims struct:

```go
type Claims struct {
    Sub  string `json:"sub"`
    Role string `json:"role"` // viewer|operator|admin
    Exp  int64  `json:"exp,omitempty"` // unix seconds
}
```

### Parser/validator: ParseAndValidateHS256(token, secret)

1. Split token into three parts by `"."`.
2. Decode base64url header and payload.
3. Check alg is exactly "HS256" (prevents alg confusion).
4. Recompute HMAC‑SHA256 over `header.payload` with the shared secret and compare with the signature using constant‑time `hmac.Equal`.
5. Unmarshal claims, check expiry (exp) if present; returns `ErrExpired` if past.
6. Returns `*Claims` on success.

### Role precedence helper

```go
func RoleOrder(r string) int { /* viewer:1, operator:2, admin:3 */ }
```

---

## Where verification is enforced (the middleware)

File: `/internal/api/api.go`

- Every API route that needs auth is wrapped with withAuth(...):

```go
mux.HandleFunc("/v1/rules",  a.withAuth(a.handleRules))
mux.HandleFunc("/v1/alarms", a.withAuth(a.handleAlarms))
mux.HandleFunc("/v1/alarms/", a.withAuth(a.handleAlarmOps))
```

- **Dev mode:** If JWT_SECRET is empty, the middleware attaches default claims `{sub:"dev", role:"admin"}` to the request context and allows the request (this is clearly logged at startup).
- **Auth mode:** If JWT_SECRET is set:
    - Extracts Authorization header, checks for Bearer  prefix (case‑insensitive).
    - Validates token via auth.ParseAndValidateHS256.
    - On success, stashes *Claims in the request context.
    - On failure, returns 401 Unauthorized.

Role enforcement per endpoint

The API uses a helper `requireRole(w, r, "role")` to gate specific operations:
- **Rules**
    - GET /v1/rules → viewer+
    - POST /v1/rules → admin
- **Alarms**
    - GET /v1/alarms → viewer+
    - POST /v1/alarms/{id}/ack → operator+
    - POST /v1/alarms/{id}/silence → operator+
    - POST /v1/alarms/{id}/clear → operator+

If the role is insufficient:
- Returns 403 Forbidden (you’re authenticated, but not allowed).

Actor propagation
- The API calls into the Manager with the actor set to claims.Sub (or "unknown" if missing):

```go
actor := actorFrom(r) // pulls claims.Sub
m.Ack(ctx, alarmID, actor) // and similar for Silence, Clear
```

- The Manager persists an AlarmEvent (e.g., acked, silenced, cleared) with that actor. This gives you an audit trail (“who did what, when”).

---

## What is not authenticated today
- POST /v1/ingest is not wrapped with the JWT middleware in Phase‑3 (it remains open for now). This was a conscious choice to keep data ingestion simple during early development.
    - If you want ingest protected:
        - Wrap /v1/ingest with withAuth and require at least viewer, or
        - Add an ingest token approach (separate shared key) or mTLS for producers.
- /debug/pprof/* is behind the PPROF_ENABLED=true flag (disabled by default), but not JWT‑protected. That’s fine for local profiling; in prod you’d typically keep it off or bind it to localhost / admin network.

---

## Configuration knobs
- JWT_SECRET
When set: JWT verification on; when unset: dev mode with admin claims for all API calls (warning logged).
- Everything else is orthogonal to auth (engine, DB, etc.).

---

## Security behaviors & best practices
- Symmetric key: Both server and token generator share the same JWT_SECRET. Use a high‑entropy secret (32+ random bytes).
- Token lifetime: The code enforces exp if present; good practice is to keep short lifetimes (e.g., 15–60 minutes).
- TLS: Always terminate TLS in front of the app so Bearer tokens aren’t exposed in cleartext.
- Clock skew: The parser uses time.Now().Unix() and doesn’t allow skew, so if systems have time drift, exp could fail early/late. Keep clocks in sync (NTP).
- Minimal claims: There’s no issuer/audience validation and no nbf/iat checks. That’s fine for internal systems; we can add those if needed.
- No key rotation: Single secret today. If you need rotation, we can support multiple keys (kid → secret map) and accept either during a transition window.
- Error handling:
    - Missing/invalid token → 401
    - Authenticated but insufficient role → 403

---

## Endpoint‑by‑endpoint matrix

| Endpoint | Auth? | Role | Notes |
|----------|-------|------|-------|
| POST /v1/ingest | No | N/A | Open (for now). Optional to wrap with JWT. |
| GET /v1/rules | Yes | viewer+ | Lists rules. |
| POST /v1/rules | Yes | admin | Creates a rule. |
| GET /v1/alarms | Yes | viewer+ | Lists alarms. |
| POST /v1/alarms/{id}/ack | Yes | operator+ | Acknowledges an alarm. |
| POST /v1/alarms/{id}/silence | Yes | operator+ | Silences an alarm until until or for ttl_sec. |
| POST /v1/alarms/{id}/clear | Yes | operator+ | Clears an alarm (useful for latched rules). |
| /debug/pprof/* (if enabled) | Flag‑gated | N/A | Controlled by PPROF_ENABLED; keep off in prod. |
| /healthz, /readyz, /version | No | N/A | Always open; safe to expose. |


---

## Common pitfalls & troubleshooting
- “missing bearer token” (401) → header absent or not Bearer ....
- “invalid token” (401) → bad signature, malformed base64, or alg not HS256.
- “token expired” (401) → exp in the past.
- “forbidden” (403) → valid token but role too low for the endpoint.
- Everything works without tokens → JWT_SECRET is empty (dev mode); set it to enable auth.

---

## Quick suggestions (if you want to tighten things)
- Protect /v1/ingest with JWT (viewer+ or a separate “producer” role).
- Add issuer/audience checks to claims for stronger guarantees.
- Support multiple keys to allow secret rotation (kid header -> secret lookup).
- Add small grace window for clock skew (± a few seconds) if needed.
- Wrap pprof behind the same middleware (and/or bind to localhost only) if you need it in prod.
