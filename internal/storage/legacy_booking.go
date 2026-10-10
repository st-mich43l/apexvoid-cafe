package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/st-mich43l/apexvoid-photobooth/internal/domain"
)

// LegacyBookings keeps the original booking API operational while Enterprise
// reviews the additive Phase 2 migration. It must never query Phase 2 columns.
func (s *Store) LegacyBookings(ctx context.Context, workspace string) ([]domain.Booking, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT b.id::text,b.booth_id::text,booth.name,b.package_id::text,
 b.guest_name,b.package_name,b.start_at,b.end_at,b.status,b.price_vnd,b.created_at,b.updated_at
 FROM photobooth_bookings b JOIN photobooth_booths booth ON booth.id=b.booth_id AND booth.workspace_id=b.workspace_id
 WHERE b.workspace_id=$1 ORDER BY b.start_at DESC LIMIT 100`, workspace)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	items := []domain.Booking{}
	for rows.Next() {
		var item domain.Booking
		if err := rows.Scan(&item.ID, &item.BoothID, &item.BoothName, &item.PackageID, &item.GuestName, &item.PackageName, &item.Start, &item.End, &item.Status, &item.PriceVND, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, MapError(err)
		}
		if item.Status == "reserved" {
			item.Status = domain.BookingConfirmed
		}
		item.PartySize = 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) legacyBooking(ctx context.Context, workspace, id string) (domain.Booking, error) {
	var item domain.Booking
	err := s.DB.QueryRowContext(ctx, `SELECT b.id::text,b.booth_id::text,booth.name,b.package_id::text,
 b.guest_name,b.package_name,b.start_at,b.end_at,b.status,b.price_vnd,b.created_at,b.updated_at
 FROM photobooth_bookings b JOIN photobooth_booths booth ON booth.id=b.booth_id AND booth.workspace_id=b.workspace_id
 WHERE b.workspace_id=$1 AND b.id=$2`, workspace, id).Scan(
		&item.ID, &item.BoothID, &item.BoothName, &item.PackageID, &item.GuestName,
		&item.PackageName, &item.Start, &item.End, &item.Status, &item.PriceVND,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	if item.Status == "reserved" {
		item.Status = domain.BookingConfirmed
	}
	item.PartySize = 1
	return item, nil
}

func (s *Store) LegacyReserve(ctx context.Context, workspace, actor string, input domain.BookingInput) (domain.Booking, error) {
	input, err := input.Validate(time.Now().UTC())
	if err != nil {
		return domain.Booking{}, err
	}
	// Migration 001 restricts booking duration to two hours.
	if input.End.Sub(input.Start) > 2*time.Hour {
		return domain.Booking{}, domain.ErrInvalid
	}
	var id string
	err = s.DB.QueryRowContext(ctx, `INSERT INTO photobooth_bookings(workspace_id,booth_id,package_id,guest_name,start_at,end_at,package_name,price_vnd,created_by)
 SELECT $1,booth.id,item.id,$4,$5,$6,item.name,item.price_vnd,$7
 FROM photobooth_booths booth JOIN photobooth_items item ON item.workspace_id=booth.workspace_id
 WHERE booth.workspace_id=$1 AND booth.id=$2 AND booth.active AND item.id=$3 AND item.active AND item.kind='photo'
 RETURNING id::text`, workspace, input.BoothID, input.PackageID, input.GuestName, input.Start, input.End, actor).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrInvalid
	}
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	return s.legacyBooking(ctx, workspace, id)
}

func (s *Store) LegacyTransition(ctx context.Context, workspace, id, action string) (domain.Booking, error) {
	if !domain.ValidID(id) {
		return domain.Booking{}, domain.ErrInvalid
	}
	var from, to string
	switch action {
	case "check-in":
		from, to = "reserved", "checked_in"
	case "complete":
		from, to = "checked_in", "completed"
	case "cancel":
		from, to = "reserved", "cancelled"
	default:
		return domain.Booking{}, domain.ErrInvalid
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE photobooth_bookings SET status=$3,updated_at=now()
 WHERE workspace_id=$1 AND id=$2 AND status=$4
 AND ($5::boolean=FALSE OR (start_at <= now()+interval '10 minutes' AND end_at>now()))`, workspace, id, to, from, action == "check-in")
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return domain.Booking{}, domain.ErrConflict
	}
	return s.legacyBooking(ctx, workspace, id)
}
