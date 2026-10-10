//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/st-mich43l/apexvoid-cafe/internal/domain"
	"github.com/st-mich43l/apexvoid-cafe/internal/storage"
)

func uuid(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}

func TestBookingMigrationsAndConcurrency(t *testing.T) {
	url := os.Getenv("APEXVOID_CAFE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set APEXVOID_CAFE_TEST_DATABASE_URL to a disposable PostgreSQL 16 test database")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(12)
	var dbName string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&dbName); err != nil {
		t.Fatal(err)
	}
	if dbName != "apexvoid_cafe_test" {
		t.Fatalf("refusing schema destructive test outside apexvoid_cafe_test: %s", dbName)
	}
	if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS cafe CASCADE; CREATE SCHEMA cafe;"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS cafe CASCADE") }()
	apply := func(name string) {
		t.Helper()
		sqlBytes, err := os.ReadFile("../../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
	apply("001_cafe.sql")
	store := storage.New(db)
	ready, err := store.AdvancedBookingReady(ctx)
	if err != nil || ready {
		t.Fatalf("Phase 2 unexpectedly enabled before migration 002: ready=%v err=%v", ready, err)
	}
	workspace, actor, booth, item := uuid(t), uuid(t), uuid(t), uuid(t)
	if _, err := db.ExecContext(ctx, "INSERT INTO cafe.cafe_booths(id,workspace_id,name,created_by) VALUES($1,$2,'Studio A',$3)", booth, workspace, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO cafe.cafe_items(id,workspace_id,sku,name,kind,price_vnd,created_by) VALUES($1,$2,'PHOTO-A','Photo Session','photo',100000,$3)", item, workspace, actor); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(time.Now().In(loc).Year(), time.Now().In(loc).Month(), time.Now().In(loc).Day(), 13, 0, 0, 0, loc).AddDate(0, 0, 2)
	legacyID := uuid(t)
	if _, err := db.ExecContext(ctx, "INSERT INTO cafe.cafe_bookings(id,workspace_id,booth_id,package_id,guest_name,start_at,end_at,package_name,price_vnd,created_by) VALUES($1,$2,$3,$4,'Legacy',$5,$6,'Photo Session',100000,$7)", legacyID, workspace, booth, item, start, start.Add(20*time.Minute), actor); err != nil {
		t.Fatal(err)
	}
	// The staged Café image must continue operating with migration 001.
	legacyItems, err := store.Items(ctx, workspace)
	if err != nil || len(legacyItems) != 1 || legacyItems[0].DurationMinutes != 20 {
		t.Fatalf("legacy menu availability: %+v %v", legacyItems, err)
	}
	if _, err := store.CreateItem(ctx, workspace, actor, domain.ItemInput{Name: "Coffee", SKU: "COFFEE-A", Kind: "drink", PriceVND: 35000}); err != nil {
		t.Fatalf("legacy menu create unavailable: %v", err)
	}
	legacyInput := domain.BookingInput{BoothID: booth, PackageID: item, GuestName: "Before approval", Start: start.Add(time.Hour), End: start.Add(time.Hour + 20*time.Minute)}
	if booking, err := store.LegacyReserve(ctx, workspace, actor, legacyInput); err != nil || booking.Status != "confirmed" {
		t.Fatalf("legacy booking create unavailable: %+v %v", booking, err)
	}
	legacyList, err := store.LegacyBookings(ctx, workspace)
	if err != nil || len(legacyList) < 2 {
		t.Fatalf("legacy bookings unavailable: %+v %v", legacyList, err)
	}
	apply("002_advanced_booking.sql")
	ready, err = store.AdvancedBookingReady(ctx)
	if err != nil || !ready {
		t.Fatalf("Phase 2 failed to activate after approved schema: ready=%v err=%v", ready, err)
	}
	var ref, status string
	if err := db.QueryRowContext(ctx, "SELECT booking_ref,status FROM cafe.cafe_bookings WHERE id=$1", legacyID).Scan(&ref, &status); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "CAF-") || status != "confirmed" {
		t.Fatalf("legacy backfill failed: %q %q", ref, status)
	}
	// A generated reference and the upgraded status default must be present
	// even though application INSERT statements omit those two columns.
	future := start.Add(90 * time.Minute)
	input := domain.BookingInput{BoothID: booth, PackageID: item, GuestName: "First", PartySize: 2, Start: future, End: future.Add(20 * time.Minute)}
	created, err := store.CreateBooking(ctx, workspace, actor, input)
	if err != nil || !strings.HasPrefix(created.BookingRef, "CAF-") || created.Status != "confirmed" {
		t.Fatalf("new booking: %+v %v", created, err)
	}
	// Concurrent writers must serialize at the database trigger, not just
	// rely on an application-level availability preview.
	concurrent := domain.BookingInput{BoothID: booth, PackageID: item, GuestName: "Race", PartySize: 1, Start: future.Add(90 * time.Minute), End: future.Add(110 * time.Minute)}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := store.CreateBooking(ctx, workspace, actor, concurrent)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if e == domain.ErrConflict {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent insert failure: %v", e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("expected one committed booking and one conflict, got %d / %d", wins, conflicts)
	}
	// Rescheduling into an occupied slot must preserve the original booking
	// and never partially write a new timestamp or activity event.
	shifted := input
	shifted.Start, shifted.End = concurrent.Start, concurrent.End
	if _, err := store.RescheduleBooking(ctx, workspace, actor, created.ID, shifted); err != domain.ErrConflict {
		t.Fatalf("conflicting reschedule unexpectedly succeeded: %v", err)
	}
	unchanged, err := store.GetBooking(ctx, workspace, created.ID)
	if err != nil || !unchanged.Start.Equal(input.Start) {
		t.Fatalf("conflicting reschedule changed original: %+v %v", unchanged, err)
	}
	idempotent := domain.BookingInput{BoothID: booth, PackageID: item, GuestName: "Repeated request", Start: start.Add(6 * time.Hour), End: start.Add(6*time.Hour + 20*time.Minute), IdempotencyKey: "stable-create-request"}
	first, err := store.CreateBooking(ctx, workspace, actor, idempotent)
	if err != nil {
		t.Fatalf("first idempotent booking: %v", err)
	}
	repeated, err := store.CreateBooking(ctx, workspace, actor, idempotent)
	if err != nil || repeated.ID != first.ID {
		t.Fatalf("idempotent retry created another booking: %+v %v", repeated, err)
	}
	mismatched := idempotent
	mismatched.GuestName = "Different guest"
	if _, err := store.CreateBooking(ctx, workspace, actor, mismatched); err != domain.ErrConflict {
		t.Fatalf("accepted idempotency key reused for a different guest: %v", err)
	}
	weekday := int(start.Weekday())
	if _, err := db.ExecContext(ctx, "INSERT INTO cafe.cafe_operating_schedules(workspace_id,weekday,buffer_before_minutes,buffer_after_minutes,created_by) VALUES($1,$2,10,10,$3)", workspace, weekday, actor); err != nil {
		t.Fatal(err)
	}
	holdStart := start.Add(4 * time.Hour)
	hold, err := store.CreateHold(ctx, workspace, actor, domain.HoldInput{BoothID: booth, PackageID: item, Start: holdStart, End: holdStart.Add(20 * time.Minute), TTLSeconds: 600})
	if err != nil {
		t.Fatalf("buffered hold: %v", err)
	}
	adjacent := domain.BookingInput{BoothID: booth, PackageID: item, GuestName: "Too close", Start: holdStart.Add(20 * time.Minute), End: holdStart.Add(40 * time.Minute)}
	if _, err := store.CreateBooking(ctx, workspace, actor, adjacent); err != domain.ErrConflict {
		t.Fatalf("adjacent reservation bypassed hold buffers: %v", err)
	}
	confirmed, err := store.ConfirmHold(ctx, workspace, actor, hold.ID, domain.BookingInput{GuestName: "Held guest", PartySize: 1})
	if err != nil || confirmed.Status != "confirmed" || confirmed.BufferBefore != 10 || confirmed.BufferAfter != 10 {
		t.Fatalf("hold confirmation failed: %+v %v", confirmed, err)
	}
	retried, err := store.ConfirmHold(ctx, workspace, actor, hold.ID, domain.BookingInput{GuestName: "Held guest"})
	if err != nil || retried.ID != confirmed.ID {
		t.Fatalf("hold confirmation is not idempotent: %+v %v", retried, err)
	}
}
