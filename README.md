# ApexVoid Café & Photo Booth

A **standalone business application** for a café combined with a self-service photo-booth venue. This is **not part of the `apexvoid-enterprise` source tree**. Deploy it in its own Docker stack and register it as an **external application** inside ApexVoid Enterprise.

**Stack:** Go 1.25 · React 19 / TypeScript / Vite · Enterprise-provisioned PostgreSQL 16 · Docker Compose.

## Architecture

```text
Browser (signed in to ApexVoid Enterprise)
    │
    ├── /apps/cafe/            → Enterprise authenticated gateway → cafe:8090/ (React)
    └── /api/apps/cafe/v1/...  → Enterprise authenticated gateway → cafe:8090/v1/... (Go API)
                                                                         │
                       For EVERY operation:                              ├── shared Enterprise PostgreSQL
                       POST Enterprise integration /session/introspect ←──┘
                       with service credential + short-lived identity assertion
```

The business app does **not** store Enterprise user passwords, sessions, user-role assignments, or copies of Enterprise's database. All workspace IDs and actors originate in operation-specific Enterprise introspection decisions. The Go API port is not published to the host; access is through the gateway only. Enterprise issues the assertion and enforces the registered application's availability and entry policy.

The Café service has no PostgreSQL container and never creates databases, roles, schemas, extensions, or tables at runtime. Enterprise provisions the canonical `apexvoid_cafe` database, `cafe` schema, and restricted `apexvoid_cafe` role during approved enrollment, applies this repository's signed migration bundle, then sends the encrypted credentials to the Café service.

**Important catalog boundary:** Enterprise's built-in ERP catalog is not directly accessible through the external integration v1 business-data API. This app therefore owns its initial *café-specific menu* and *photo package catalog*. Syncing orders with ERP accounting/inventory requires future explicit, authorized service-to-service business APIs; do **not** access the Enterprise PostgreSQL tables.

## Implemented MVP

- **Café counter:** configure drinks, create multi-line orders, exact integer VND totals, track open/served/cancelled. Order line prices are snapshotted transactionally from the owned catalog.
- **Photo booths:** configure shooting stations, create timed sessions using configured photo packages, check in and complete or cancel bookings.
- **No double-booking:** PostgreSQL GiST exclusion constraint rejects overlap for a given booth, even during concurrent requests. Cancelled and completed slots are excluded; adjacent time intervals are allowed.
- **Unified interface:** responsive React UI with light/dark toggle, order dashboard, POS counter, booth calendar list, and menu setup.
- **Security:** every API operation requires `X-ApexVoid-Identity-Assertion` from Enterprise and performs its own permission-specific introspection. Workspace/user supplied in payloads are rejected. Tenant isolation uses workspace IDs returned by Enterprise.

**Intentionally not built:** payment capture, legal receipts/e-invoices, advanced drink sizes/modifiers, photo capture/printing/file storage, booking reminders, staff schedules, cash drawers, inventory accounting, public bookings, refunds, or reliable cross-app events. An order marked **served** is not evidence of payment.

## Getting started — 2 separate repositories

1. Create an empty GitHub repository named **`apexvoid-cafe`**. Nothing needs to be copied into the Enterprise source tree.
2. In the Enterprise environment create the shared network once:

   ```bash
   docker network create apexvoid-apps
   ```

3. Give Enterprise's existing **backend and postgres** services access to that network. Compose the Enterprise stack with `deploy/enterprise-network.override.yml` supplied in this project. Set a unique, long `INTEGRATIONS_ASSERTION_SECRET`, a trusted `DATABASE_PROVISIONING_URL`, and a stable 32+ character `DATABASE_PROVISIONING_KEY`; ensure `INTEGRATIONS_ALLOWED_SERVICE_HOSTS` includes `cafe` (and preserve other already-allowed hosts if configured).
4. In this project, copy `.env.example` to `.env` and start the independent Café service:

   ```bash
   cp .env.example .env
   docker compose up -d --build
   ```

   On first start, Docker logs show the service URL, manifest URL, and one high-entropy one-time enrollment code. The persistent `cafe-bootstrap-state` volume stores this state with restrictive permissions. No database password or permanent service credential is printed.
5. Sign in to Enterprise as a platform administrator and open **Applications → Register application**. Enter the Café service URL and one-time enrollment code in the registration popup. Enterprise discovers the signed manifest, previews permissions and migrations, provisions the shared PostgreSQL database/schema/role, applies the migration, encrypts the credentials, and completes enrollment automatically. Approve all requested registration, permissions, database, schema, and migration operations.
6. After enrollment, the Café logs show only a concise activation/verification status. Restarting the service reuses the persisted protected enrollment state and verifies the shared database; it does not print the code again.
7. Enable the application for the target workspace via Enterprise application management. Assign staff the appropriate `cafe.*` workspace permissions. Browse to **`/apps/cafe/` on the Enterprise origin**. Frontend requests use `/api/apps/cafe/v1/...`; no second login is required.

   Example of workspace availability API, for an Enterprise admin:

   ```text
   PUT /api/v1/applications/external/cafe/workspaces/{workspace-id}
   {"enabled": true}
   ```

   The service cannot self-assign permission grants. Any role changes are performed inside Enterprise.

## Registration contract

Registration is driven by the signed `/.well-known/apexvoid/manifest.json` and the encrypted `/.well-known/apexvoid/enroll` exchange. The old [`deploy/enterprise-registration.json`](deploy/enterprise-registration.json) file is retained only as a historical reference and is not an installation input. Contract is strictly `api_contract_version=v1`.

The current Enterprise master decoder strictly accepts permission entries using its Go wire names (`Name`, `DisplayName`, `Description`, `Scope`); Café emits those exact keys so discovery succeeds without an Enterprise source change.

| Permission | Purpose |
|---|---|
| `cafe.catalog.read` | Browse menu |
| `cafe.catalog.manage` | Create drinks and photo packages |
| `cafe.order.read` | View orders |
| `cafe.order.manage` | Create, serve or cancel orders |
| `cafe.booking.read` | View booths and sessions |
| `cafe.booking.manage` | Reserve, check in, complete or cancel sessions |
| `cafe.booth.manage` | Create new booths |

The application entry policy is **any** of menu/order/booking read. Each API endpoint checks its *own* permission through introspection. The UI has limited permission awareness: operations without grants will be rejected by the backend, and administrators should grant the role's intended operations together for a complete experience.

## Development and tests

```bash
go test ./...       # Requires Go module downloads on first run
cd web && npm install && npm run typecheck && npm run build
```

For local testing, only the domain, gateway-client and HTTP authorization tests can run without external dependencies:

```bash
go test ./internal/domain ./internal/platform ./internal/service
```

The Go image build downloads `pgx/v5` and the public Enterprise integration SDK; the frontend image build downloads npm dependencies. Enterprise owns the migration ledger and applies version 1 in a transaction guarded by an advisory lock. The Café only verifies the expected database identity and schema after enrollment. Any future schema changes should use a new migration version, not edit the applied SQL. Existing legacy `cafe-db-data` volumes are not removed automatically; back them up and migrate deliberately before deleting them.

**Do not publish port 8090 directly** while using the Enterprise assertion-based trust model. There is no stand-alone login for direct access; the Enterprise gateway is required.

## Business model notes

This is a **staff-operated proof of concept** for combining a café and self-photo booths. Photo packages are scheduled experiences; café drinks are repeat purchases; future cross-sells include drink+photo combos, premium frames, reprints and group bookings. Validate utilization, staffing, booth turnover, drink margins and equipment depreciation before treating the concept as profitable. Photo images are not stored, and only a guest name is stored for a booking. Before public operation define personal-data retention and privacy notices, and check Vietnam's current rules for invoicing, customer data and business licensing.
