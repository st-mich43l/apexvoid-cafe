# ApexVoid Photobooth

A **standalone business application** for self-service photo-booth reservations and venue operations, with an optional café counter. This is **not part of the `apexvoid-enterprise` source tree**. Deploy it in its own Docker stack and register it as an **external application** inside ApexVoid Enterprise.

## Project declaration

**ApexVoid Photobooth** is the product and repository identity. It manages photo-booth packages, booths, reservations, guest flow, operating schedules, and an optional café counter from an Enterprise-connected workspace. This full technical rename introduces `photobooth` as a new Enterprise application identity with its own service host, gateway route, permission namespace, PostgreSQL database, schema, role, and migration bundle. Existing `cafe` registrations and `apexvoid_cafe` data are not modified in place; keep them separate until a deliberate migration and decommission plan has been approved.

**Stack:** Go 1.25 · React 19 / TypeScript / Vite · Enterprise-provisioned PostgreSQL 16 · Docker Compose.

## Architecture

```text
Browser (signed in to ApexVoid Enterprise)
    │
    ├── /apps/photobooth/            → Enterprise authenticated gateway → photobooth:8090/ (React)
    └── /api/apps/photobooth/v1/...  → Enterprise authenticated gateway → photobooth:8090/v1/... (Go API)
                                                                         │
                       For EVERY operation:                              ├── shared Enterprise PostgreSQL
                       POST Enterprise integration /session/introspect ←──┘
                       with service credential + short-lived identity assertion
```

The business app does **not** store Enterprise user passwords, sessions, user-role assignments, or copies of Enterprise's database. All workspace IDs and actors originate in operation-specific Enterprise introspection decisions. The Go API port is not published to the host; access is through the gateway only. Enterprise issues the assertion and enforces the registered application's availability and entry policy.

The Photobooth service has no PostgreSQL container and never creates databases, roles, schemas, extensions, or tables at runtime. Enterprise provisions the canonical `apexvoid_photobooth` database, `photobooth` schema, and restricted `apexvoid_photobooth` role during approved enrollment, applies this repository's signed migration bundle, then sends the encrypted credentials to the Photobooth service.

**Important catalog boundary:** Enterprise's built-in ERP catalog is not directly accessible through the external integration v1 business-data API. This app therefore owns its initial *café-specific menu* and *photo package catalog*. Syncing orders with ERP accounting/inventory requires future explicit, authorized service-to-service business APIs; do **not** access the Enterprise PostgreSQL tables.

## Implemented booking management

Phase 2 extends the original photo-booth proof of concept with:

- Confirmed → checked-in → in-progress → completed lifecycle with cancellation and no-show branches.
- Backend availability calculated from workspace schedules, package duration, active bookings, temporary holds, blackouts, minimum notice and future horizon.
- Short-lived holds, transactional hold confirmation, idempotency keys, and PostgreSQL advisory-lock overlap protection including preparation/cleanup buffers.
- Searchable booking history with immutable activity events, guest contact fields, party size, notes, and status timestamps.
- Day/week booking calendar, operational dashboard, lifecycle controls, history timeline, operating-hour editor, and blackout management.

## Guest Care Desk

The **Guest care** navigation provides a customer-service workflow for venue
staff without introducing a separate CRM or storing Enterprise user accounts:

- **Today’s service queue:** confirmed arrivals, checked-in guests, and sessions
  in progress, always calculated in the venue’s `Asia/Ho_Chi_Minh` timezone
  regardless of which week staff last viewed on the booking calendar.
- **Search across booking history:** enter at least two characters of the
  guest’s name, phone, email, or reservation reference. Results come from the
  workspace-isolated, paginated booking API, rather than only the visible week.
- **Guest booking drawer:** direct phone/email links when contact details are
  available, reference copy, package, party size, preparation notes, activity
  events, rescheduling, and lifecycle actions.
- **Accountable cancellations:** staff are prompted to record a reason for
  cancellations and no-shows; the existing booking activity ledger records the
  actor and reason on successful transitions.
- **Responsive design:** touch-friendly call/open-booking actions, compact
  service cards, accessible loading and error states, and a mobile detail sheet.

Permission checks remain on the server: reading reservations requires
`photobooth.booking.read`, reading historical events requires
`photobooth.booking.history.read`, and modifying bookings requires
`photobooth.booking.manage`. Frontend affordances are not authorization checks.
Guest contact details are only shown after Enterprise gateway authorization
and are never included in public links or unauthenticated responses.

This is a **reservation-focused care desk**, not a unified CRM customer profile:
repeated guest names or phone numbers are not automatically merged into a
customer identity; messaging/reminders are not sent automatically.

## Implemented MVP

- **Photobooth counter:** configure drinks, create multi-line orders, exact integer VND totals, track open/served/cancelled. Order line prices are snapshotted transactionally from the owned catalog.
- **Photo booths:** configure shooting stations, create timed sessions using configured photo packages, check in, start, complete, cancel, or mark bookings as no-shows.
- **No double-booking:** PostgreSQL transaction-scoped advisory locks plus schema triggers reject overlapping buffered bookings and active holds, even during concurrent requests. Cancelled, completed and no-show slots are excluded; adjacent time intervals are allowed.
- **Unified interface:** responsive React UI with light/dark toggle, order dashboard, POS counter, booth calendar list, and menu setup.
- **Security:** every API operation requires `X-ApexVoid-Identity-Assertion` from Enterprise and performs its own permission-specific introspection. Workspace/user supplied in payloads are rejected. Tenant isolation uses workspace IDs returned by Enterprise.

**Intentionally not built:** payment capture, legal receipts/e-invoices, advanced drink sizes/modifiers, photo capture/printing/file storage, booking reminders, staff schedules, cash drawers, inventory accounting, public bookings, refunds, or reliable cross-app events. An order marked **served** is not evidence of payment.

## Getting started — 2 separate repositories

1. Create an empty GitHub repository named **`apexvoid-photobooth`**. Nothing needs to be copied into the Enterprise source tree.
2. In the Enterprise environment create the two shared networks once:

   ```bash
   docker network create apexvoid-apps
   docker network create apexvoid-data
   ```

3. Give Enterprise's **backend** access to both networks and **postgres** access to `apexvoid-data`. Compose the Enterprise stack with `deploy/enterprise-network.override.yml` supplied in this project. Set a unique, long `INTEGRATIONS_ASSERTION_SECRET`, a trusted `DATABASE_PROVISIONING_URL` using the Docker hostname `postgres` (not `localhost`), and a stable 32+ character `DATABASE_PROVISIONING_KEY`. For a fresh local Enterprise PostgreSQL volume, set `DATABASE_PROVISIONER_PASSWORD`; Enterprise's init script creates the restricted `apexvoid_provisioner` administrator required for approved database provisioning. The signed manifest, one-time enrollment code, and platform-admin approval authorize this service; no Photobooth-specific Enterprise host environment variable is required.
4. In this project, copy `.env.example` to `.env` and start the independent Photobooth service:

   ```bash
   cp .env.example .env
   docker compose up -d --build
   ```

   On first start, Docker logs show the service URL, manifest URL, and one high-entropy one-time enrollment code. The persistent `photobooth-bootstrap-state` volume stores this state with restrictive permissions. No database password or permanent service credential is printed.
5. Sign in to Enterprise as a platform administrator and open **Applications → Register application**. Enter the Photobooth service URL and one-time enrollment code in the registration popup. Enterprise discovers the signed manifest, previews permissions and migrations, provisions the shared PostgreSQL database/schema/role, applies the migration, encrypts the credentials, and completes enrollment automatically. Approve all requested registration, permissions, database, schema, and migration operations.
6. After enrollment, the Photobooth logs show only a concise activation/verification status. Restarting the service reuses the persisted protected enrollment state and verifies the shared database; it does not print the code again.
7. Enable the application for the target workspace via Enterprise application management. Assign staff the appropriate `photobooth.*` workspace permissions. Browse to **`/apps/photobooth/` on the Enterprise origin**. Frontend requests use `/api/apps/photobooth/v1/...`; no second login is required.

   Example of workspace availability API, for an Enterprise admin:

   ```text
   PUT /api/v1/applications/external/photobooth/workspaces/{workspace-id}
   {"enabled": true}
   ```

   The service cannot self-assign permission grants. Any role changes are performed inside Enterprise.

## Registration contract

Registration is driven by the signed `/.well-known/apexvoid/manifest.json` and the encrypted `/.well-known/apexvoid/enroll` exchange. The old [`deploy/enterprise-registration.json`](deploy/enterprise-registration.json) file is retained only as a historical reference and is not an installation input. Contract is strictly `api_contract_version=v1`.

The current Enterprise master decoder strictly accepts permission entries using its Go wire names (`Name`, `DisplayName`, `Description`, `Scope`); Photobooth emits those exact keys so discovery succeeds without an Enterprise source change.

| Permission | Purpose |
|---|---|
| `photobooth.catalog.read` | Browse menu |
| `photobooth.catalog.manage` | Create drinks and photo packages |
| `photobooth.order.read` | View orders |
| `photobooth.order.manage` | Create, serve or cancel orders |
| `photobooth.booking.read` | View booths and sessions |
| `photobooth.booking.manage` | Reserve, check in, start, complete, cancel or mark sessions |
| `photobooth.booking.history.read` | View immutable booking activity |
| `photobooth.booking.schedule.manage` | Configure hours, closures and blackouts |
| `photobooth.booth.manage` | Create new booths |

The application entry policy is **any** of menu/order/booking read, booking history read, or schedule management. Each API endpoint checks its *own* permission through introspection. The UI has limited permission awareness: operations without grants will be rejected by the backend, and administrators should grant the role's intended operations together for a complete experience.

### Booking API

The booking API is workspace-scoped by the Enterprise identity assertion. Important routes include `GET /v1/bookings`, `GET /v1/bookings/availability`, `POST /v1/bookings`, `POST /v1/booking-holds`, `POST /v1/booking-holds/{id}/confirm`, `POST /v1/bookings/{id}/reschedule`, lifecycle actions under `/v1/bookings/{id}/`, `GET /v1/bookings/{id}/events`, `/v1/schedules`, `/v1/blackouts`, and `/v1/booths/utilization`. Conflicts return HTTP 409; invalid transitions are rejected without changing the booking.

## Development and tests

```bash
go test ./...       # Requires Go module downloads on first run
cd web && npm install && npm run typecheck && npm run build
```

For local testing, only the domain, gateway-client and HTTP authorization tests can run without external dependencies:

```bash
go test ./internal/domain ./internal/platform ./internal/service
```

The Go image build downloads `pgx/v5` and the public Enterprise integration SDK; the frontend image build downloads npm dependencies. Enterprise owns the migration ledger and applies versions 1 and 2 in order in a transaction guarded by an advisory lock. The Photobooth publishes every immutable SQL artifact under `/.well-known/apexvoid/migrations/` and only verifies the expected database identity/schema after the approved upgrade is applied. Any future schema changes should use a new migration version, not edit an applied SQL file. Existing legacy `photobooth-db-data` volumes are not removed automatically; back them up and migrate deliberately before deleting them.

For a staged Phase 2 release, deploy the new Photobooth image first. The original schema remains sufficient for service activation. The existing menu, counter, booth and legacy reservation endpoints stay operational through a Phase 1-compatible storage path; new scheduling, holds, availability and advanced booking operations return `SCHEMA_UPGRADE_REQUIRED` until Enterprise approves and applies `002_advanced_booking.sql`. The new calendar/form UI depends on advanced availability, so arrange prompt approval or retain access to the previous booking UI during the rollout window. The container never creates another database or self-applies migrations. Once approved, the same running service begins serving Phase 2 operations.

**Do not publish port 8090 directly** while using the Enterprise assertion-based trust model. There is no stand-alone login for direct access; the Enterprise gateway is required.

## Business model notes

This is a **staff-operated proof of concept** for combining a café and self-photo booths. Photo packages are scheduled experiences; café drinks are repeat purchases; future cross-sells include drink+photo combos, premium frames, reprints and group bookings. Validate utilization, staffing, booth turnover, drink margins and equipment depreciation before treating the concept as profitable. Photo images are not stored; booking records can include guest names, optional phone/email, party size and staff notes. Before public operation define personal-data retention and privacy notices, and check Vietnam's current rules for invoicing, customer data and business licensing.

## Phase 1 Enterprise application lifecycle compatibility

Photobooth now supports the Enterprise **reviewed application upgrade** manifest
challenge. An enrolled Photobooth service answers
`GET /.well-known/apexvoid/manifest.json` with the exact JSON manifest and
`X-ApexVoid-Update-Signature` only when Enterprise supplies a fresh
`X-ApexVoid-Update-Challenge` header. Signature input is the existing
enrollment-persisted permanent service credential (SHA-256-derived HMAC key),
the domain-separated update context, the challenge and the raw manifest bytes.
No permanent credential, enrollment code or database secret is sent over HTTP.
The old signed enrollment response remains available for initial registration.

The Enterprise application gateway now uses full-page /apps/photobooth/ navigation,
not an iframe. A selected workspace is passed on the initial app launch and
resolved against the current user's membership. Photobooth includes that selection
in its API headers, so orders and bookings remain bound to the same workspace.

To introduce a later Photobooth schema or permission upgrade: append a **new**
numbered SQL migration, increment the stable application version and, when SQL
changes, its migration bundle version. Keep all previously published migration
versions, paths and checksums identical, and do not change application database
ownership. Enterprise must explicitly approve the signed plan and its new
permissions/migrations. Keep newly deployed code backward-compatible with the
existing schema until approval finishes. POS integration is not in scope.

### Phase 2 database verification

GitHub CI now runs `go test -tags=integration ./tests/integration` against a disposable PostgreSQL 16 database. The test applies the original and new migrations in order, checks historical booking backfills and generated references, verifies legacy read/write behavior while approval is pending, and exercises concurrent bookings, hold buffers, idempotent confirmation and rescheduling rollback.

A separate Enterprise fix is required before applying migration 002: the Enterprise migration safety filter must accept ordinary `UPDATE ... SET` data backfills without allowing privilege/session changes. Apply Enterprise PR #40 (or its merged equivalent) before approving this Photobooth schema upgrade. The migration bundle version is `0.2.0` (semantic version), while SQL migration numbers are the integers 1 and 2.
