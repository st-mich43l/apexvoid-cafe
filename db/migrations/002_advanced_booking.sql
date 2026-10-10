-- ApexVoid Café Phase 2: advanced booking management.
-- This file is immutable once approved by Enterprise. It extends migration 001;
-- it never replaces or edits the original artifact.

ALTER TABLE cafe.cafe_bookings
  ADD COLUMN IF NOT EXISTS booking_ref VARCHAR(24),
  ADD COLUMN IF NOT EXISTS guest_phone VARCHAR(40) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS guest_email VARCHAR(254) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS party_size INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS notes VARCHAR(1000) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS addons JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS buffer_before_minutes INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS buffer_after_minutes INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS actual_checked_in_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS session_started_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cancellation_reason VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS no_show_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS no_show_reason VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS updated_by UUID,
  ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(128);

ALTER TABLE cafe.cafe_items
  ADD COLUMN IF NOT EXISTS duration_minutes INTEGER NOT NULL DEFAULT 20;
ALTER TABLE cafe.cafe_items DROP CONSTRAINT IF EXISTS cafe_items_duration_minutes_check;
ALTER TABLE cafe.cafe_items ADD CONSTRAINT cafe_items_duration_minutes_check CHECK(duration_minutes BETWEEN 5 AND 480);

ALTER TABLE cafe.cafe_bookings DROP CONSTRAINT IF EXISTS cafe_bookings_status_check;
ALTER TABLE cafe.cafe_bookings DROP CONSTRAINT IF EXISTS cafe_bookings_party_size_check;
ALTER TABLE cafe.cafe_bookings DROP CONSTRAINT IF EXISTS cafe_bookings_buffer_before_minutes_check;
ALTER TABLE cafe.cafe_bookings DROP CONSTRAINT IF EXISTS cafe_bookings_buffer_after_minutes_check;
UPDATE cafe.cafe_bookings SET status='confirmed' WHERE status='reserved';
UPDATE cafe.cafe_bookings
SET booking_ref='CAF-' || upper(substr(replace(id::text, '-', ''), 1, 10))
WHERE booking_ref IS NULL OR booking_ref='';

ALTER TABLE cafe.cafe_bookings ALTER COLUMN booking_ref SET NOT NULL;
ALTER TABLE cafe.cafe_bookings
  ADD CONSTRAINT cafe_bookings_status_check CHECK(status IN ('confirmed','checked_in','in_progress','completed','cancelled','no_show')),
  ADD CONSTRAINT cafe_bookings_party_size_check CHECK(party_size BETWEEN 1 AND 100),
  ADD CONSTRAINT cafe_bookings_buffer_before_minutes_check CHECK(buffer_before_minutes BETWEEN 0 AND 240),
  ADD CONSTRAINT cafe_bookings_buffer_after_minutes_check CHECK(buffer_after_minutes BETWEEN 0 AND 240);
CREATE UNIQUE INDEX IF NOT EXISTS cafe_bookings_ref_unique ON cafe.cafe_bookings(workspace_id, booking_ref);
CREATE UNIQUE INDEX IF NOT EXISTS cafe_bookings_idempotency_unique ON cafe.cafe_bookings(workspace_id, idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
CREATE INDEX IF NOT EXISTS cafe_bookings_search ON cafe.cafe_bookings(workspace_id, start_at, status, booth_id);

CREATE TABLE IF NOT EXISTS cafe.cafe_booking_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 booking_id UUID NOT NULL,
 actor_id UUID NOT NULL,
 event_type VARCHAR(40) NOT NULL,
 from_status VARCHAR(20) NOT NULL DEFAULT '',
 to_status VARCHAR(20) NOT NULL DEFAULT '',
 reason VARCHAR(500) NOT NULL DEFAULT '',
 changes JSONB NOT NULL DEFAULT '{}'::jsonb,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(booking_id) REFERENCES cafe.cafe_bookings(id) ON DELETE CASCADE,
 UNIQUE(id,workspace_id)
);
CREATE INDEX IF NOT EXISTS cafe_booking_events_lookup ON cafe.cafe_booking_events(workspace_id,booking_id,created_at DESC);

CREATE TABLE IF NOT EXISTS cafe.cafe_booking_holds (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 booth_id UUID NOT NULL,
 package_id UUID NOT NULL,
 start_at TIMESTAMPTZ NOT NULL,
 end_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK(status IN ('active','confirmed','released','expired')),
 confirmed_booking_id UUID,
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 released_at TIMESTAMPTZ,
 UNIQUE(id,workspace_id),
 FOREIGN KEY(booth_id,workspace_id) REFERENCES cafe.cafe_booths(id,workspace_id),
 FOREIGN KEY(package_id,workspace_id) REFERENCES cafe.cafe_items(id,workspace_id),
 CHECK(end_at > start_at),
 CHECK(expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS cafe_booking_holds_active ON cafe.cafe_booking_holds(workspace_id,booth_id,start_at) WHERE status='active';

CREATE TABLE IF NOT EXISTS cafe.cafe_booking_addons (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 booking_id UUID NOT NULL,
 addon_name VARCHAR(160) NOT NULL,
 quantity INTEGER NOT NULL DEFAULT 1 CHECK(quantity BETWEEN 1 AND 99),
 unit_price_vnd BIGINT NOT NULL DEFAULT 0 CHECK(unit_price_vnd >= 0),
 FOREIGN KEY(booking_id) REFERENCES cafe.cafe_bookings(id) ON DELETE CASCADE,
 UNIQUE(id,workspace_id)
);

CREATE TABLE IF NOT EXISTS cafe.cafe_operating_schedules (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 booth_id UUID,
 weekday SMALLINT NOT NULL CHECK(weekday BETWEEN 0 AND 6),
 open_time TIME NOT NULL DEFAULT '09:00',
 close_time TIME NOT NULL DEFAULT '21:00',
 closed BOOLEAN NOT NULL DEFAULT FALSE,
 slot_increment_minutes INTEGER NOT NULL DEFAULT 15 CHECK(slot_increment_minutes BETWEEN 5 AND 120),
 min_advance_minutes INTEGER NOT NULL DEFAULT 30 CHECK(min_advance_minutes BETWEEN 0 AND 10080),
 max_horizon_days INTEGER NOT NULL DEFAULT 90 CHECK(max_horizon_days BETWEEN 1 AND 730),
 buffer_before_minutes INTEGER NOT NULL DEFAULT 0 CHECK(buffer_before_minutes BETWEEN 0 AND 240),
 buffer_after_minutes INTEGER NOT NULL DEFAULT 0 CHECK(buffer_after_minutes BETWEEN 0 AND 240),
 timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
 created_by UUID NOT NULL,
 updated_by UUID,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,booth_id,weekday),
 FOREIGN KEY(booth_id,workspace_id) REFERENCES cafe.cafe_booths(id,workspace_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cafe.cafe_schedule_exceptions (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 booth_id UUID,
 local_date DATE NOT NULL,
 closed BOOLEAN NOT NULL DEFAULT TRUE,
 open_time TIME,
 close_time TIME,
 reason VARCHAR(300) NOT NULL DEFAULT '',
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,booth_id,local_date),
 FOREIGN KEY(booth_id,workspace_id) REFERENCES cafe.cafe_booths(id,workspace_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cafe.cafe_booth_blackouts (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 booth_id UUID,
 start_at TIMESTAMPTZ NOT NULL,
 end_at TIMESTAMPTZ NOT NULL,
 reason VARCHAR(300) NOT NULL,
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(end_at > start_at),
 FOREIGN KEY(booth_id,workspace_id) REFERENCES cafe.cafe_booths(id,workspace_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS cafe_booth_blackouts_lookup ON cafe.cafe_booth_blackouts(workspace_id,booth_id,start_at);

-- Replace the Phase 1 trigger with the buffered booking/hold lock. This keeps
-- concurrent Go instances correct without requiring the restricted role to
-- install btree_gist.
CREATE OR REPLACE FUNCTION cafe.cafe_booking_no_overlap()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN ('confirmed','checked_in','in_progress') THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.booth_id::text, 72840021));
  UPDATE cafe.cafe_booking_holds SET status='expired', released_at=now()
   WHERE workspace_id=NEW.workspace_id AND booth_id=NEW.booth_id AND status='active' AND expires_at <= now();
  IF EXISTS (
   SELECT 1 FROM cafe.cafe_bookings existing
    WHERE existing.booth_id=NEW.booth_id AND existing.workspace_id=NEW.workspace_id
      AND existing.status IN ('confirmed','checked_in','in_progress') AND existing.id <> NEW.id
      AND tstzrange(existing.start_at - make_interval(mins => existing.buffer_before_minutes), existing.end_at + make_interval(mins => existing.buffer_after_minutes), '[)')
          && tstzrange(NEW.start_at - make_interval(mins => NEW.buffer_before_minutes), NEW.end_at + make_interval(mins => NEW.buffer_after_minutes), '[)')
  ) OR EXISTS (
   SELECT 1 FROM cafe.cafe_booking_holds hold
    WHERE hold.workspace_id=NEW.workspace_id AND hold.booth_id=NEW.booth_id AND hold.status='active' AND hold.expires_at > now()
      AND tstzrange(hold.start_at,hold.end_at,'[)') && tstzrange(NEW.start_at - make_interval(mins => NEW.buffer_before_minutes), NEW.end_at + make_interval(mins => NEW.buffer_after_minutes), '[)')
  ) THEN
   RAISE EXCEPTION 'booking overlaps an existing booking or hold' USING ERRCODE='23P01';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS cafe_booking_no_overlap ON cafe.cafe_bookings;
CREATE TRIGGER cafe_booking_no_overlap
 BEFORE INSERT OR UPDATE OF booth_id,workspace_id,start_at,end_at,status,buffer_before_minutes,buffer_after_minutes
 ON cafe.cafe_bookings FOR EACH ROW EXECUTE FUNCTION cafe.cafe_booking_no_overlap();

CREATE OR REPLACE FUNCTION cafe.cafe_hold_no_overlap()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='active' AND NEW.expires_at > now() THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.booth_id::text, 72840021));
  UPDATE cafe.cafe_booking_holds SET status='expired', released_at=now()
   WHERE workspace_id=NEW.workspace_id AND booth_id=NEW.booth_id AND status='active' AND expires_at <= now();
  IF EXISTS (
   SELECT 1 FROM cafe.cafe_bookings b WHERE b.workspace_id=NEW.workspace_id AND b.booth_id=NEW.booth_id
    AND b.status IN ('confirmed','checked_in','in_progress')
    AND tstzrange(b.start_at - make_interval(mins => b.buffer_before_minutes), b.end_at + make_interval(mins => b.buffer_after_minutes),'[)') && tstzrange(NEW.start_at,NEW.end_at,'[)')
  ) OR EXISTS (
   SELECT 1 FROM cafe.cafe_booking_holds h WHERE h.workspace_id=NEW.workspace_id AND h.booth_id=NEW.booth_id AND h.id<>NEW.id AND h.status='active' AND h.expires_at>now()
    AND tstzrange(h.start_at,h.end_at,'[)') && tstzrange(NEW.start_at,NEW.end_at,'[)')
  ) THEN
   RAISE EXCEPTION 'hold overlaps an existing booking or hold' USING ERRCODE='23P01';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS cafe_hold_no_overlap ON cafe.cafe_booking_holds;
CREATE TRIGGER cafe_hold_no_overlap BEFORE INSERT OR UPDATE OF booth_id,start_at,end_at,status,expires_at
 ON cafe.cafe_booking_holds FOR EACH ROW EXECUTE FUNCTION cafe.cafe_hold_no_overlap();

INSERT INTO cafe.cafe_booking_events(workspace_id,booking_id,actor_id,event_type,to_status,reason)
 SELECT workspace_id,id,created_by,'legacy_import',status,'Imported from Phase 1 booking record'
 FROM cafe.cafe_bookings b
 WHERE NOT EXISTS (SELECT 1 FROM cafe.cafe_booking_events e WHERE e.booking_id=b.id);
