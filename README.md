# ApexVoid Café & Photo Booth

A **standalone business application** for a café combined with a self-service photo-booth venue. This is **not part of the `apexvoid-enterprise` source tree**. Deploy it in its own Docker stack and register it as an **external application** inside ApexVoid Enterprise.

**Stack:** Go 1.23 · React 19 / TypeScript / Vite · PostgreSQL 16 · Docker Compose.

## Architecture

```text
Browser (signed in to ApexVoid Enterprise)
    │
    ├── /apps/cafe/            → Enterprise authenticated gateway → cafe:8090/ (React)
    └── /api/apps/cafe/v1/...  → Enterprise authenticated gateway → cafe:8090/v1/... (Go API)
                                                                         │
                       For EVERY operation:                              ├── private cafe-db (PostgreSQL)
                       POST Enterprise integration /session/introspect ←──┘
                       with service credential + short-lived identity assertion
```

The business app does **not** store Enterprise user passwords, sessions, user-role assignments, or copies of Enterprise's database. All workspace IDs and actors originate in operation-specific Enterprise introspection decisions. The Go API port is not published to the host; access is through the gateway only. Enterprise issues the assertion and enforces the registered application's availability and entry policy.

**Important catalog boundary:** Enterprise's built-in ERP catalog is not directly accessible through the external integration v1 business-data API. This app therefore owns its initial *café-specific menu* and *photo package catalog*. Syncing orders with ERP accounting/inventory requires future explicit, authorized service-to-service business APIs; do **not** access the Enterprise PostgreSQL tables.

## Implemented MVP

- **Café counter:** configure drinks, create multi-line orders, exact integer VND totals, track open/served/cancelled. Order line prices are snapshotted transactionally from the owned catalog.
- **Photo booths:** configure shooting stations, create timed sessions using configured photo packages, check in and complete or cancel bookings.
- **No double-booking:** PostgreSQL GiST exclusion constraint rejects overlap for a given booth, even during concurrent requests. Cancelled and completed slots are excluded; adjacent time intervals are allowed.
- **Unified interface:** responsive React UI with light/dark toggle, order dashboard, POS counter, booth calendar list, and menu setup.
- **Security:** every API operation requires `X-ApexVoid-Identity-Assertion` from Enterprise and performs its own permission-specific introspection. Workspace/user supplied in payloads are rejected. Tenant isolation uses workspace IDs returned by Enterprise.

**Intentionally not built:** payment capture, legal receipts/e-invoices, advanced drink sizes/modifiers, photo capture/printing/file storage, booking reminders, staff schedules, cash drawers, inventory accounting, public bookings, refunds, or reliable cross-app events. An order marked **served** is not evidence of payment.

## Getting started — 2 separate repositories

1. Create an empty GitHub repository named **`apexvoid-cafe`**. Copy this project's files to that repository. Nothing needs to be copied into the Enterprise source tree.
2. In the Enterprise environment create the shared network once:

   ```bash
   docker network create apexvoid-apps
   ```

3. Give Enterprise's existing **backend** access to that network. Compose the Enterprise stack with `deploy/enterprise-network.override.yml` supplied in this project. Set a unique, long `INTEGRATIONS_ASSERTION_SECRET`; ensure `INTEGRATIONS_ALLOWED_SERVICE_HOSTS` includes `cafe` (and preserve other already-allowed hosts if configured).
4. In this **new project**, copy `.env.example` to `.env`, set a strong database password, and start the independent café containers:

   ```bash
   cp .env.example .env
   docker compose up -d --build
   ```

   The service may start with a blank `APEXVOID_SERVICE_CREDENTIAL` to expose its health endpoint for registration. All protected API operations will fail closed until the credential is installed.
5. Sign in to Enterprise as platform administrator. Use its **Applications → Register application** flow, filling in the values from `deploy/enterprise-registration.json`. Only a trusted admin should register this service. Confirm the private Docker hostname `cafe` is allowed.
6. Registration returns a `service_credential` **once**. Put it in the new repo's runtime secret management (for a local development stack, `.env`), then restart the service:

   ```bash
   docker compose up -d --force-recreate cafe
   ```

7. Enable the application for the target workspace via Enterprise application management. Assign staff the appropriate `cafe.*` workspace permissions. Browse to **`/apps/cafe/` on the Enterprise origin**. Frontend requests use `/api/apps/cafe/v1/...`; no second login is required.

   Example of workspace availability API, for an Enterprise admin:

   ```text
   PUT /api/v1/applications/external/cafe/workspaces/{workspace-id}
   {"enabled": true}
   ```

   The service cannot self-assign permission grants. Any role changes are performed inside Enterprise.

## Registration

See [`deploy/enterprise-registration.json`](deploy/enterprise-registration.json). Contract is strictly `api_contract_version=v1`.

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

The Go image build downloads `pgx/v5`; the frontend image build downloads npm dependencies. The Docker database owns its own schema. The migration runner applies version 1 once, in a transaction guarded by an advisory lock. Any future schema changes should use a new migration version, not edit the applied SQL.

**Do not publish port 8090 directly** while using the Enterprise assertion-based trust model. There is no stand-alone login for direct access; the Enterprise gateway is required.

## Business model notes

This is a **staff-operated proof of concept** for combining a café and self-photo booths. Photo packages are scheduled experiences; café drinks are repeat purchases; future cross-sells include drink+photo combos, premium frames, reprints and group bookings. Validate utilization, staffing, booth turnover, drink margins and equipment depreciation before treating the concept as profitable. Photo images are not stored, and only a guest name is stored for a booking. Before public operation define personal-data retention and privacy notices, and check Vietnam's current rules for invoicing, customer data and business licensing.
