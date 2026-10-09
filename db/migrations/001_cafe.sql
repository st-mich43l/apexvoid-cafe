-- Each object is owned exclusively by apexvoid-cafe in the Enterprise-provisioned
-- cafe schema. The migration runs as the restricted application role.
--
-- Do not install extensions here. Enterprise deliberately rejects privileged
-- extension statements. The former btree_gist exclusion constraint is replaced
-- by the schema-local trigger at the end of this migration.
CREATE TABLE IF NOT EXISTS cafe.cafe_items (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 sku VARCHAR(64) NOT NULL,
 name VARCHAR(160) NOT NULL,
 kind VARCHAR(12) NOT NULL CHECK(kind IN ('drink','photo')),
 price_vnd BIGINT NOT NULL CHECK(price_vnd BETWEEN 0 AND 100000000),
 active BOOLEAN NOT NULL DEFAULT TRUE,
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(id,workspace_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS cafe_items_sku_unique ON cafe.cafe_items(workspace_id,lower(sku));
CREATE INDEX IF NOT EXISTS cafe_items_workspace ON cafe.cafe_items(workspace_id,kind,active);

CREATE TABLE IF NOT EXISTS cafe.cafe_orders (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'open' CHECK(status IN ('open','served','cancelled')),
 note VARCHAR(300) NOT NULL DEFAULT '',
 total_vnd BIGINT NOT NULL DEFAULT 0 CHECK(total_vnd>=0),
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(id,workspace_id)
);
CREATE INDEX IF NOT EXISTS cafe_orders_by_workspace ON cafe.cafe_orders(workspace_id,created_at DESC);
CREATE TABLE IF NOT EXISTS cafe.cafe_order_lines (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 order_id UUID NOT NULL,
 item_id UUID NOT NULL,
 item_name VARCHAR(160) NOT NULL,
 quantity INT NOT NULL CHECK(quantity BETWEEN 1 AND 99),
 unit_price_vnd BIGINT NOT NULL CHECK(unit_price_vnd>=0),
 line_total_vnd BIGINT GENERATED ALWAYS AS (quantity::BIGINT*unit_price_vnd) STORED,
 FOREIGN KEY(order_id,workspace_id) REFERENCES cafe.cafe_orders(id,workspace_id) ON DELETE CASCADE,
 FOREIGN KEY(item_id,workspace_id) REFERENCES cafe.cafe_items(id,workspace_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS cafe_order_lines_lookup ON cafe.cafe_order_lines(workspace_id,order_id);

CREATE TABLE IF NOT EXISTS cafe.cafe_booths (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 name VARCHAR(100) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(id,workspace_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS cafe_booths_name_unique ON cafe.cafe_booths(workspace_id,lower(name));

CREATE TABLE IF NOT EXISTS cafe.cafe_bookings (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 booth_id UUID NOT NULL,
 package_id UUID NOT NULL,
 guest_name VARCHAR(120) NOT NULL,
 start_at TIMESTAMPTZ NOT NULL,
 end_at TIMESTAMPTZ NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'reserved' CHECK(status IN ('reserved','checked_in','completed','cancelled')),
 package_name VARCHAR(160) NOT NULL,
 price_vnd BIGINT NOT NULL CHECK(price_vnd >= 0),
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(end_at > start_at AND end_at <= start_at + interval '2 hours'),
 FOREIGN KEY(booth_id,workspace_id) REFERENCES cafe.cafe_booths(id,workspace_id),
 FOREIGN KEY(package_id,workspace_id) REFERENCES cafe.cafe_items(id,workspace_id)
);
CREATE INDEX IF NOT EXISTS cafe_bookings_by_workspace ON cafe.cafe_bookings(workspace_id,start_at DESC);

-- Keep the former GiST exclusion semantics without requiring the restricted
-- application role to install btree_gist. The transaction-scoped advisory lock
-- serializes bookings for one booth; the overlap query preserves adjacent
-- intervals and excludes cancelled/completed bookings.
CREATE OR REPLACE FUNCTION cafe.cafe_booking_no_overlap()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
 IF NEW.status IN ('reserved','checked_in') THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.booth_id::text, 72840021));
  IF EXISTS (
   SELECT 1
   FROM cafe.cafe_bookings existing
   WHERE existing.booth_id = NEW.booth_id
     AND existing.workspace_id = NEW.workspace_id
     AND existing.status IN ('reserved','checked_in')
     AND existing.id <> NEW.id
     AND tstzrange(existing.start_at, existing.end_at, '[)') && tstzrange(NEW.start_at, NEW.end_at, '[)')
  ) THEN
   RAISE EXCEPTION 'booking overlaps an existing booking' USING ERRCODE = '23P01';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS cafe_booking_no_overlap ON cafe.cafe_bookings;
CREATE TRIGGER cafe_booking_no_overlap
BEFORE INSERT OR UPDATE OF booth_id, workspace_id, start_at, end_at, status
ON cafe.cafe_bookings
FOR EACH ROW EXECUTE FUNCTION cafe.cafe_booking_no_overlap();
