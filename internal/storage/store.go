package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/st-mich43l/apexvoid-cafe/internal/domain"
)

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }
func MapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		switch state.SQLState() {
		case "23505", "23P01", "23503":
			return domain.ErrConflict
		case "23514", "22003", "22P02":
			return domain.ErrInvalid
		}
	}
	return err
}
func (s *Store) Items(ctx context.Context, workspace string) ([]domain.Item, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,name,sku,kind,price_vnd,duration_minutes,active FROM cafe_items WHERE workspace_id=$1 ORDER BY kind,name LIMIT 200`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Item{}
	for rows.Next() {
		var x domain.Item
		if err := rows.Scan(&x.ID, &x.Name, &x.SKU, &x.Kind, &x.PriceVND, &x.DurationMinutes, &x.Active); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) CreateItem(ctx context.Context, workspace, actor string, input domain.ItemInput) (domain.Item, error) {
	input, err := input.Normalize()
	if err != nil {
		return domain.Item{}, err
	}
	var x domain.Item
	err = s.DB.QueryRowContext(ctx, `INSERT INTO cafe_items(workspace_id,name,sku,kind,price_vnd,duration_minutes,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text,name,sku,kind,price_vnd,duration_minutes,active`, workspace, input.Name, input.SKU, input.Kind, input.PriceVND, input.DurationMinutes, actor).Scan(&x.ID, &x.Name, &x.SKU, &x.Kind, &x.PriceVND, &x.DurationMinutes, &x.Active)
	return x, MapError(err)
}
func (s *Store) Booths(ctx context.Context, workspace string) ([]domain.Booth, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,name,active FROM cafe_booths WHERE workspace_id=$1 ORDER BY name`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Booth{}
	for rows.Next() {
		var x domain.Booth
		if err := rows.Scan(&x.ID, &x.Name, &x.Active); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) CreateBooth(ctx context.Context, workspace, actor, name string) (domain.Booth, error) {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 100 {
		return domain.Booth{}, domain.ErrInvalid
	}
	var x domain.Booth
	err := s.DB.QueryRowContext(ctx, `INSERT INTO cafe_booths(workspace_id,name,created_by) VALUES($1,$2,$3) RETURNING id::text,name,active`, workspace, name, actor).Scan(&x.ID, &x.Name, &x.Active)
	return x, MapError(err)
}
func (s *Store) Orders(ctx context.Context, workspace string) ([]domain.Order, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,status,total_vnd,note,created_at FROM cafe_orders WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 100`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Order{}
	for rows.Next() {
		var x domain.Order
		if err := rows.Scan(&x.ID, &x.Status, &x.TotalVND, &x.Note, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) GetOrder(ctx context.Context, workspace, id string) (domain.Order, error) {
	var x domain.Order
	err := s.DB.QueryRowContext(ctx, `SELECT id::text,status,total_vnd,note,created_at FROM cafe_orders WHERE workspace_id=$1 AND id=$2`, workspace, id).Scan(&x.ID, &x.Status, &x.TotalVND, &x.Note, &x.CreatedAt)
	return x, MapError(err)
}
func (s *Store) CreateOrder(ctx context.Context, workspace, actor string, input domain.OrderInput) (domain.Order, error) {
	input, err := input.Validate()
	if err != nil {
		return domain.Order{}, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO cafe_orders(workspace_id,created_by,note) VALUES($1,$2,$3) RETURNING id::text`, workspace, actor, input.Note).Scan(&id)
	if err != nil {
		return domain.Order{}, MapError(err)
	}
	for _, line := range input.Lines {
		var name string
		var price int64
		err = tx.QueryRowContext(ctx, `SELECT name,price_vnd FROM cafe_items WHERE workspace_id=$1 AND id=$2 AND kind='drink' AND active FOR SHARE`, workspace, line.ItemID).Scan(&name, &price)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Order{}, domain.ErrInvalid
		}
		if err != nil {
			return domain.Order{}, MapError(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO cafe_order_lines(workspace_id,order_id,item_id,item_name,quantity,unit_price_vnd) VALUES($1,$2,$3,$4,$5,$6)`, workspace, id, line.ItemID, name, line.Quantity, price)
		if err != nil {
			return domain.Order{}, MapError(err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE cafe_orders SET total_vnd=(SELECT coalesce(sum(line_total_vnd),0)::bigint FROM cafe_order_lines WHERE workspace_id=$1 AND order_id=$2) WHERE workspace_id=$1 AND id=$2`, workspace, id)
	if err != nil {
		return domain.Order{}, MapError(err)
	}
	var out domain.Order
	err = tx.QueryRowContext(ctx, `SELECT id::text,status,total_vnd,note,created_at FROM cafe_orders WHERE workspace_id=$1 AND id=$2`, workspace, id).Scan(&out.ID, &out.Status, &out.TotalVND, &out.Note, &out.CreatedAt)
	if err != nil {
		return domain.Order{}, MapError(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.Order{}, MapError(err)
	}
	return out, nil
}
func (s *Store) TransitionOrder(ctx context.Context, workspace, id, action string) (domain.Order, error) {
	if !domain.ValidID(id) || (action != "serve" && action != "cancel") {
		return domain.Order{}, domain.ErrInvalid
	}
	status := "served"
	if action == "cancel" {
		status = "cancelled"
	}
	var out domain.Order
	err := s.DB.QueryRowContext(ctx, `UPDATE cafe_orders SET status=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND status='open' RETURNING id::text,status,total_vnd,note,created_at`, workspace, id, status).Scan(&out.ID, &out.Status, &out.TotalVND, &out.Note, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, domain.ErrConflict
	}
	return out, MapError(err)
}

const bookingFields = `b.id::text,b.booking_ref,b.workspace_id::text,b.booth_id::text,booth.name,b.package_id::text,b.guest_name,b.guest_phone,b.guest_email,b.package_name,b.start_at,b.end_at,b.status,b.price_vnd,b.party_size,b.notes,b.addons,b.buffer_before_minutes,b.buffer_after_minutes,b.actual_checked_in_at,b.session_started_at,b.completed_at,b.cancelled_at,b.cancellation_reason,b.no_show_at,b.no_show_reason,b.created_at,b.updated_at`

type rowScanner interface{ Scan(...any) error }

func scanBooking(row rowScanner) (domain.Booking, error) {
	var x domain.Booking
	var addons []byte
	var checkedIn, started, completed, cancelled, noShow sql.NullTime
	err := row.Scan(&x.ID, &x.BookingRef, &x.WorkspaceID, &x.BoothID, &x.BoothName, &x.PackageID, &x.GuestName, &x.GuestPhone, &x.GuestEmail, &x.PackageName, &x.Start, &x.End, &x.Status, &x.PriceVND, &x.PartySize, &x.Notes, &addons, &x.BufferBefore, &x.BufferAfter, &checkedIn, &started, &completed, &cancelled, &x.CancellationReason, &noShow, &x.NoShowReason, &x.CreatedAt, &x.UpdatedAt)
	if err != nil {
		return x, MapError(err)
	}
	if len(addons) > 0 {
		_ = json.Unmarshal(addons, &x.Addons)
	}
	if checkedIn.Valid {
		x.ActualCheckedInAt = &checkedIn.Time
	}
	if started.Valid {
		x.SessionStartedAt = &started.Time
	}
	if completed.Valid {
		x.CompletedAt = &completed.Time
	}
	if cancelled.Valid {
		x.CancelledAt = &cancelled.Time
	}
	if noShow.Valid {
		x.NoShowAt = &noShow.Time
	}
	return x, nil
}

func (s *Store) Bookings(ctx context.Context, workspace string) ([]domain.Booking, error) {
	page, err := s.ListBookings(ctx, workspace, domain.BookingFilter{Page: 1, PageSize: 100})
	return page.Items, err
}

func (s *Store) ListBookings(ctx context.Context, workspace string, filter domain.BookingFilter) (domain.Page[domain.Booking], error) {
	filter, err := filter.Normalize()
	if err != nil {
		return domain.Page[domain.Booking]{}, err
	}
	where := []string{"b.workspace_id=$1"}
	args := []any{workspace}
	next := 2
	if !filter.From.IsZero() {
		where = append(where, fmt.Sprintf("b.end_at > $%d", next))
		args = append(args, filter.From)
		next++
	}
	if !filter.To.IsZero() {
		where = append(where, fmt.Sprintf("b.start_at < $%d", next))
		args = append(args, filter.To)
		next++
	}
	if filter.BoothID != "" {
		where = append(where, fmt.Sprintf("b.booth_id=$%d", next))
		args = append(args, filter.BoothID)
		next++
	}
	if filter.Status != "" {
		where = append(where, fmt.Sprintf("b.status=$%d", next))
		args = append(args, filter.Status)
		next++
	}
	if filter.Guest != "" {
		where = append(where, fmt.Sprintf("(b.guest_name ILIKE $%d OR b.guest_phone ILIKE $%d)", next, next))
		args = append(args, "%"+filter.Guest+"%")
		next++
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM cafe_bookings b WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return domain.Page[domain.Booking]{}, MapError(err)
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+bookingFields+` FROM cafe_bookings b JOIN cafe_booths booth ON booth.id=b.booth_id AND booth.workspace_id=b.workspace_id WHERE `+whereSQL+` ORDER BY b.start_at DESC LIMIT $`+strconv.Itoa(next)+` OFFSET $`+strconv.Itoa(next+1), args...)
	if err != nil {
		return domain.Page[domain.Booking]{}, MapError(err)
	}
	defer rows.Close()
	items := make([]domain.Booking, 0)
	for rows.Next() {
		item, scanErr := scanBooking(rows)
		if scanErr != nil {
			return domain.Page[domain.Booking]{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.Page[domain.Booking]{}, err
	}
	return domain.Page[domain.Booking]{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total, HasMore: filter.Page*filter.PageSize < total}, nil
}

func (s *Store) GetBooking(ctx context.Context, workspace, id string) (domain.Booking, error) {
	if !domain.ValidID(id) {
		return domain.Booking{}, domain.ErrInvalid
	}
	return scanBooking(s.DB.QueryRowContext(ctx, `SELECT `+bookingFields+` FROM cafe_bookings b JOIN cafe_booths booth ON booth.id=b.booth_id AND booth.workspace_id=b.workspace_id WHERE b.workspace_id=$1 AND b.id=$2`, workspace, id))
}

func (s *Store) slotAllowed(ctx context.Context, tx *sql.Tx, workspace, booth string, start, end time.Time) error {
	var timezone, open, close string
	var closed bool
	var minAdvance, horizon int
	weekday := int(start.In(time.FixedZone("ICT", 7*60*60)).Weekday())
	err := tx.QueryRowContext(ctx, `SELECT timezone,to_char(open_time,'HH24:MI'),to_char(close_time,'HH24:MI'),closed,min_advance_minutes,max_horizon_days FROM cafe_operating_schedules WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) AND weekday=$3 ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, booth, weekday).Scan(&timezone, &open, &close, &closed, &minAdvance, &horizon)
	if errors.Is(err, sql.ErrNoRows) {
		timezone, open, close, minAdvance, horizon = "Asia/Ho_Chi_Minh", "09:00", "21:00", 30, 90
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return MapError(err)
	}
	loc, locErr := time.LoadLocation(timezone)
	if locErr != nil {
		loc, _ = time.LoadLocation("Asia/Ho_Chi_Minh")
	}
	localStart, localEnd := start.In(loc), end.In(loc)
	if closed || localStart.Format("15:04") < open || localEnd.Format("15:04") > close || localStart.Before(time.Now().In(loc).Add(time.Duration(minAdvance)*time.Minute)) || localStart.After(time.Now().In(loc).AddDate(0, 0, horizon)) {
		return domain.ErrConflict
	}
	var exceptionClosed bool
	var exceptionOpen, exceptionClose string
	err = tx.QueryRowContext(ctx, `SELECT closed,coalesce(to_char(open_time,'HH24:MI'),''),coalesce(to_char(close_time,'HH24:MI'),'') FROM cafe_schedule_exceptions WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) AND local_date=$3 ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, booth, localStart.Format("2006-01-02")).Scan(&exceptionClosed, &exceptionOpen, &exceptionClose)
	if err == nil {
		if exceptionClosed || (exceptionOpen != "" && (localStart.Format("15:04") < exceptionOpen || localEnd.Format("15:04") > exceptionClose)) {
			return domain.ErrConflict
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return MapError(err)
	}
	var blocked bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cafe_booth_blackouts WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) AND tstzrange(start_at,end_at,'[)') && tstzrange($3,$4,'[)'))`, workspace, booth, start, end).Scan(&blocked)
	if err != nil {
		return MapError(err)
	}
	if blocked {
		return domain.ErrConflict
	}
	return nil
}

func packageDuration(ctx context.Context, tx *sql.Tx, workspace, packageID string, start, end time.Time) error {
	var minutes int
	err := tx.QueryRowContext(ctx, `SELECT duration_minutes FROM cafe_items WHERE workspace_id=$1 AND id=$2 AND kind='photo' AND active`, workspace, packageID).Scan(&minutes)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrInvalid
	}
	if err != nil {
		return MapError(err)
	}
	if end.Sub(start) != time.Duration(minutes)*time.Minute {
		return domain.ErrInvalid
	}
	return nil
}

func applyScheduleBuffers(ctx context.Context, tx *sql.Tx, workspace, booth string, start time.Time, input *domain.BookingInput) error {
	if input.BufferBefore != 0 || input.BufferAfter != 0 {
		return nil
	}
	var timezone string
	err := tx.QueryRowContext(ctx, `SELECT timezone FROM cafe_operating_schedules WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, booth).Scan(&timezone)
	if errors.Is(err, sql.ErrNoRows) {
		timezone = "Asia/Ho_Chi_Minh"
	} else if err != nil {
		return MapError(err)
	}
	loc, loadErr := time.LoadLocation(timezone)
	if loadErr != nil {
		loc, _ = time.LoadLocation("Asia/Ho_Chi_Minh")
	}
	var before, after int
	err = tx.QueryRowContext(ctx, `SELECT buffer_before_minutes,buffer_after_minutes FROM cafe_operating_schedules WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) AND weekday=$3 ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, booth, int(start.In(loc).Weekday())).Scan(&before, &after)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return MapError(err)
	}
	input.BufferBefore, input.BufferAfter = before, after
	return nil
}

func (s *Store) CreateBooking(ctx context.Context, workspace, actor string, input domain.BookingInput) (domain.Booking, error) {
	input, err := input.Validate(time.Now().UTC())
	if err != nil {
		return domain.Booking{}, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return domain.Booking{}, err
	}
	defer tx.Rollback()
	if input.IdempotencyKey != "" {
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM cafe_bookings WHERE workspace_id=$1 AND idempotency_key=$2`, workspace, input.IdempotencyKey).Scan(&existing)
		if err == nil {
			_ = tx.Rollback()
			return s.GetBooking(ctx, workspace, existing)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.Booking{}, MapError(err)
		}
	}
	if err = packageDuration(ctx, tx, workspace, input.PackageID, input.Start, input.End); err != nil {
		return domain.Booking{}, err
	}
	if err = applyScheduleBuffers(ctx, tx, workspace, input.BoothID, input.Start, &input); err != nil {
		return domain.Booking{}, err
	}
	if err = s.slotAllowed(ctx, tx, workspace, input.BoothID, input.Start, input.End); err != nil {
		return domain.Booking{}, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO cafe_bookings(workspace_id,booth_id,package_id,guest_name,guest_phone,guest_email,party_size,notes,addons,start_at,end_at,package_name,price_vnd,buffer_before_minutes,buffer_after_minutes,created_by,updated_by,idempotency_key)
 SELECT $1,booth.id,item.id,$4,$5,$6,$7,$8,$9,$10,$11,item.name,item.price_vnd,$12,$13,$14,$14,$15
 FROM cafe_booths booth JOIN cafe_items item ON item.workspace_id=booth.workspace_id
 WHERE booth.workspace_id=$1 AND booth.id=$2 AND booth.active AND item.id=$3 AND item.active AND item.kind='photo' RETURNING id::text`, workspace, input.BoothID, input.PackageID, input.GuestName, input.GuestPhone, input.GuestEmail, input.PartySize, input.Notes, jsonOrEmpty(input.Addons), input.Start, input.End, input.BufferBefore, input.BufferAfter, actor, input.IdempotencyKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrInvalid
	}
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cafe_booking_events(workspace_id,booking_id,actor_id,event_type,to_status) VALUES($1,$2,$3,'created','confirmed')`, workspace, id, actor); err != nil {
		return domain.Booking{}, MapError(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.Booking{}, MapError(err)
	}
	return s.GetBooking(ctx, workspace, id)
}

func jsonOrEmpty(values []string) []byte {
	if values == nil {
		return []byte("[]")
	}
	b, _ := json.Marshal(values)
	return b
}

func (s *Store) Reserve(ctx context.Context, workspace, actor string, input domain.BookingInput) (domain.Booking, error) {
	return s.CreateBooking(ctx, workspace, actor, input)
}

func (s *Store) TransitionBookingAdvanced(ctx context.Context, workspace, actor, id, action, reason string) (domain.Booking, error) {
	if !domain.ValidID(id) {
		return domain.Booking{}, domain.ErrInvalid
	}
	to := map[string]string{"check-in": domain.BookingCheckedIn, "start": domain.BookingInProgress, "complete": domain.BookingCompleted, "cancel": domain.BookingCancelled, "no-show": domain.BookingNoShow}[action]
	if to == "" {
		return domain.Booking{}, domain.ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return domain.Booking{}, err
	}
	defer tx.Rollback()
	var from string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM cafe_bookings WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&from); errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrNotFound
	} else if err != nil {
		return domain.Booking{}, MapError(err)
	}
	if from == to {
		_ = tx.Rollback()
		return s.GetBooking(ctx, workspace, id)
	}
	if !domain.CanTransitionBooking(from, to) {
		return domain.Booking{}, domain.ErrConflict
	}
	var query string
	switch to {
	case domain.BookingCheckedIn:
		query = `UPDATE cafe_bookings SET status=$3,actual_checked_in_at=coalesce(actual_checked_in_at,now()),updated_by=$4,updated_at=now() WHERE workspace_id=$1 AND id=$2`
	case domain.BookingInProgress:
		query = `UPDATE cafe_bookings SET status=$3,session_started_at=coalesce(session_started_at,now()),updated_by=$4,updated_at=now() WHERE workspace_id=$1 AND id=$2`
	case domain.BookingCompleted:
		query = `UPDATE cafe_bookings SET status=$3,completed_at=coalesce(completed_at,now()),updated_by=$4,updated_at=now() WHERE workspace_id=$1 AND id=$2`
	case domain.BookingCancelled:
		query = `UPDATE cafe_bookings SET status=$3,cancelled_at=coalesce(cancelled_at,now()),cancellation_reason=$5,updated_by=$4,updated_at=now() WHERE workspace_id=$1 AND id=$2`
	case domain.BookingNoShow:
		query = `UPDATE cafe_bookings SET status=$3,no_show_at=coalesce(no_show_at,now()),no_show_reason=$5,updated_by=$4,updated_at=now() WHERE workspace_id=$1 AND id=$2`
	}
	args := []any{workspace, id, to, actor}
	if to == domain.BookingCancelled || to == domain.BookingNoShow {
		args = append(args, strings.TrimSpace(reason))
	}
	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return domain.Booking{}, MapError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cafe_booking_events(workspace_id,booking_id,actor_id,event_type,from_status,to_status,reason) VALUES($1,$2,$3,$4,$5,$6,$7)`, workspace, id, actor, action, from, to, strings.TrimSpace(reason)); err != nil {
		return domain.Booking{}, MapError(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.Booking{}, MapError(err)
	}
	return s.GetBooking(ctx, workspace, id)
}

// TransitionBooking preserves the Phase 1 storage contract for callers that
// do not yet pass an actor or reason. Phase 2 handlers use the advanced form.
func (s *Store) TransitionBooking(ctx context.Context, workspace, id, action string) (domain.Booking, error) {
	return s.TransitionBookingAdvanced(ctx, workspace, "00000000-0000-4000-8000-000000000000", id, action, "")
}

func (s *Store) RescheduleBooking(ctx context.Context, workspace, actor, id string, input domain.BookingInput) (domain.Booking, error) {
	input, err := input.Validate(time.Now().UTC())
	if err != nil {
		return domain.Booking{}, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return domain.Booking{}, err
	}
	defer tx.Rollback()
	var from string
	var oldStart, oldEnd time.Time
	var oldBooth string
	err = tx.QueryRowContext(ctx, `SELECT status,start_at,end_at,booth_id::text FROM cafe_bookings WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&from, &oldStart, &oldEnd, &oldBooth)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	if from != domain.BookingConfirmed {
		return domain.Booking{}, domain.ErrConflict
	}
	if err = packageDuration(ctx, tx, workspace, input.PackageID, input.Start, input.End); err != nil {
		return domain.Booking{}, err
	}
	if err = applyScheduleBuffers(ctx, tx, workspace, input.BoothID, input.Start, &input); err != nil {
		return domain.Booking{}, err
	}
	if err = s.slotAllowed(ctx, tx, workspace, input.BoothID, input.Start, input.End); err != nil {
		return domain.Booking{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE cafe_bookings SET booth_id=$3,package_id=$4,start_at=$5,end_at=$6,package_name=(SELECT name FROM cafe_items WHERE id=$4 AND workspace_id=$1),price_vnd=(SELECT price_vnd FROM cafe_items WHERE id=$4 AND workspace_id=$1),guest_name=$7,guest_phone=$8,guest_email=$9,party_size=$10,notes=$11,addons=$12,buffer_before_minutes=$13,buffer_after_minutes=$14,updated_by=$15,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspace, id, input.BoothID, input.PackageID, input.Start, input.End, input.GuestName, input.GuestPhone, input.GuestEmail, input.PartySize, input.Notes, jsonOrEmpty(input.Addons), input.BufferBefore, input.BufferAfter, actor)
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	changes := map[string]any{"old_booth_id": oldBooth, "new_booth_id": input.BoothID, "old_start": oldStart, "new_start": input.Start, "old_end": oldEnd, "new_end": input.End}
	raw, _ := json.Marshal(changes)
	if _, err = tx.ExecContext(ctx, `INSERT INTO cafe_booking_events(workspace_id,booking_id,actor_id,event_type,changes) VALUES($1,$2,$3,'rescheduled',$4)`, workspace, id, actor, raw); err != nil {
		return domain.Booking{}, MapError(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.Booking{}, MapError(err)
	}
	return s.GetBooking(ctx, workspace, id)
}

func (s *Store) Events(ctx context.Context, workspace, id string) ([]domain.BookingEvent, error) {
	if !domain.ValidID(id) {
		return nil, domain.ErrInvalid
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,booking_id::text,actor_id::text,event_type,from_status,to_status,reason,changes,created_at FROM cafe_booking_events WHERE workspace_id=$1 AND booking_id=$2 ORDER BY created_at ASC`, workspace, id)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []domain.BookingEvent{}
	for rows.Next() {
		var e domain.BookingEvent
		var raw []byte
		if err := rows.Scan(&e.ID, &e.BookingID, &e.ActorID, &e.EventType, &e.From, &e.To, &e.Reason, &raw, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &e.Changes)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CreateHold(ctx context.Context, workspace, actor string, input domain.HoldInput) (domain.Hold, error) {
	input, err := input.Validate(time.Now().UTC())
	if err != nil {
		return domain.Hold{}, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return domain.Hold{}, err
	}
	defer tx.Rollback()
	if err = packageDuration(ctx, tx, workspace, input.PackageID, input.Start, input.End); err != nil {
		return domain.Hold{}, err
	}
	if err = s.slotAllowed(ctx, tx, workspace, input.BoothID, input.Start, input.End); err != nil {
		return domain.Hold{}, err
	}
	var h domain.Hold
	err = tx.QueryRowContext(ctx, `INSERT INTO cafe_booking_holds(workspace_id,booth_id,package_id,start_at,end_at,expires_at,created_by) VALUES($1,$2,$3,$4,$5,now()+make_interval(secs=>$6),$7) RETURNING id::text,booth_id::text,package_id::text,start_at,end_at,expires_at,status,created_at`, workspace, input.BoothID, input.PackageID, input.Start, input.End, input.TTLSeconds, actor).Scan(&h.ID, &h.BoothID, &h.PackageID, &h.Start, &h.End, &h.ExpiresAt, &h.Status, &h.CreatedAt)
	if err != nil {
		return domain.Hold{}, MapError(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.Hold{}, MapError(err)
	}
	return h, nil
}

func (s *Store) ReleaseHold(ctx context.Context, workspace, id string) (domain.Hold, error) {
	var h domain.Hold
	err := s.DB.QueryRowContext(ctx, `UPDATE cafe_booking_holds SET status='released',released_at=now() WHERE workspace_id=$1 AND id=$2 AND status='active' RETURNING id::text,booth_id::text,package_id::text,start_at,end_at,expires_at,status,created_at`, workspace, id).Scan(&h.ID, &h.BoothID, &h.PackageID, &h.Start, &h.End, &h.ExpiresAt, &h.Status, &h.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Hold{}, domain.ErrConflict
	}
	return h, MapError(err)
}

func (s *Store) ConfirmHold(ctx context.Context, workspace, actor, id string, input domain.BookingInput) (domain.Booking, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return domain.Booking{}, err
	}
	defer tx.Rollback()
	var h domain.Hold
	err = tx.QueryRowContext(ctx, `SELECT id::text,booth_id::text,package_id::text,start_at,end_at,expires_at,status,coalesce(confirmed_booking_id::text,'') FROM cafe_booking_holds WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&h.ID, &h.BoothID, &h.PackageID, &h.Start, &h.End, &h.ExpiresAt, &h.Status, &input.IdempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	if h.Status == "confirmed" && domain.ValidID(input.IdempotencyKey) {
		_ = tx.Rollback()
		return s.GetBooking(ctx, workspace, input.IdempotencyKey)
	}
	if h.Status != "active" || !h.ExpiresAt.After(time.Now()) {
		return domain.Booking{}, domain.ErrConflict
	}
	input.BoothID, input.PackageID, input.Start, input.End = h.BoothID, h.PackageID, h.Start, h.End
	input, err = input.Validate(time.Now().UTC())
	if err != nil {
		return domain.Booking{}, err
	}
	// Release the hold inside the same transaction before inserting the booking.
	// The overlap trigger must not see the hold being converted as a conflict.
	if _, err = tx.ExecContext(ctx, `UPDATE cafe_booking_holds SET status='confirmed',released_at=now() WHERE workspace_id=$1 AND id=$2`, workspace, id); err != nil {
		return domain.Booking{}, MapError(err)
	}
	var bookingID string
	err = tx.QueryRowContext(ctx, `INSERT INTO cafe_bookings(workspace_id,booth_id,package_id,guest_name,guest_phone,guest_email,party_size,notes,addons,start_at,end_at,package_name,price_vnd,created_by,updated_by) SELECT $1,booth.id,item.id,$4,$5,$6,$7,$8,$9,$10,$11,item.name,item.price_vnd,$12,$12 FROM cafe_booths booth JOIN cafe_items item ON item.id=$3 AND item.workspace_id=$1 WHERE booth.id=$2 AND booth.workspace_id=$1 AND booth.active AND item.active AND item.kind='photo' RETURNING id::text`, workspace, input.BoothID, input.PackageID, input.GuestName, input.GuestPhone, input.GuestEmail, input.PartySize, input.Notes, jsonOrEmpty(input.Addons), input.Start, input.End, actor).Scan(&bookingID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrInvalid
	}
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cafe_booking_holds SET confirmed_booking_id=$3 WHERE workspace_id=$1 AND id=$2`, workspace, id, bookingID); err != nil {
		return domain.Booking{}, MapError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cafe_booking_events(workspace_id,booking_id,actor_id,event_type,to_status) VALUES($1,$2,$3,'created_from_hold','confirmed')`, workspace, bookingID, actor); err != nil {
		return domain.Booking{}, MapError(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.Booking{}, MapError(err)
	}
	return s.GetBooking(ctx, workspace, bookingID)
}

func (s *Store) Availability(ctx context.Context, workspace string, q domain.AvailabilityQuery) ([]domain.AvailabilitySlot, error) {
	if !domain.ValidID(q.BoothID) || !domain.ValidID(q.PackageID) {
		return nil, domain.ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var duration int
	var tz string
	err = tx.QueryRowContext(ctx, `SELECT duration_minutes FROM cafe_items WHERE workspace_id=$1 AND id=$2 AND kind='photo' AND active`, workspace, q.PackageID).Scan(&duration)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrInvalid
	}
	if err != nil {
		return nil, MapError(err)
	}
	err = tx.QueryRowContext(ctx, `SELECT timezone FROM cafe_operating_schedules WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, q.BoothID).Scan(&tz)
	if errors.Is(err, sql.ErrNoRows) {
		tz = "Asia/Ho_Chi_Minh"
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, MapError(err)
	}
	loc, e := time.LoadLocation(tz)
	if e != nil {
		loc, _ = time.LoadLocation("Asia/Ho_Chi_Minh")
	}
	day := q.Date.In(loc)
	if q.Date.IsZero() {
		day = time.Now().In(loc)
	}
	var open, close string
	var closed bool
	var increment, minAdvance, horizon, bufferBefore, bufferAfter int
	err = tx.QueryRowContext(ctx, `SELECT to_char(open_time,'HH24:MI'),to_char(close_time,'HH24:MI'),closed,slot_increment_minutes,min_advance_minutes,max_horizon_days FROM cafe_operating_schedules WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) AND weekday=$3 ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, q.BoothID, int(day.Weekday())).Scan(&open, &close, &closed, &increment, &minAdvance, &horizon)
	if errors.Is(err, sql.ErrNoRows) {
		open, close, increment, minAdvance, horizon, bufferBefore, bufferAfter = "09:00", "21:00", 15, 30, 90, 0, 0
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, MapError(err)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		_ = tx.QueryRowContext(ctx, `SELECT buffer_before_minutes,buffer_after_minutes FROM cafe_operating_schedules WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) AND weekday=$3 ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, q.BoothID, int(day.Weekday())).Scan(&bufferBefore, &bufferAfter)
	}
	var exceptionClosed bool
	var exceptionOpen, exceptionClose string
	err = tx.QueryRowContext(ctx, `SELECT closed,coalesce(to_char(open_time,'HH24:MI'),''),coalesce(to_char(close_time,'HH24:MI'),'') FROM cafe_schedule_exceptions WHERE workspace_id=$1 AND (booth_id=$2 OR booth_id IS NULL) AND local_date=$3 ORDER BY booth_id NULLS LAST LIMIT 1`, workspace, q.BoothID, day.Format("2006-01-02")).Scan(&exceptionClosed, &exceptionOpen, &exceptionClose)
	if err == nil {
		if exceptionClosed {
			return []domain.AvailabilitySlot{}, nil
		}
		if exceptionOpen != "" {
			open, close = exceptionOpen, exceptionClose
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, MapError(err)
	}
	if closed {
		return []domain.AvailabilitySlot{}, nil
	}
	start, parseStartErr := time.ParseInLocation("2006-01-02 15:04", day.Format("2006-01-02")+" "+open, loc)
	endOfDay, parseEndErr := time.ParseInLocation("2006-01-02 15:04", day.Format("2006-01-02")+" "+close, loc)
	if parseStartErr != nil || parseEndErr != nil || !endOfDay.After(start) {
		return []domain.AvailabilitySlot{}, nil
	}
	slots := []domain.AvailabilitySlot{}
	minimum := time.Now().In(loc).Add(time.Duration(minAdvance) * time.Minute)
	maximum := time.Now().In(loc).AddDate(0, 0, horizon)
	for cursor := start; cursor.Add(time.Duration(duration)*time.Minute).Before(endOfDay) || cursor.Add(time.Duration(duration)*time.Minute).Equal(endOfDay); cursor = cursor.Add(time.Duration(increment) * time.Minute) {
		slotEnd := cursor.Add(time.Duration(duration) * time.Minute)
		occupiedStart, occupiedEnd := cursor.Add(-time.Duration(bufferBefore)*time.Minute), slotEnd.Add(time.Duration(bufferAfter)*time.Minute)
		var blocked bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cafe.cafe_bookings b WHERE b.workspace_id=$1 AND b.booth_id=$2 AND b.status IN ('confirmed','checked_in','in_progress') AND tstzrange(b.start_at - make_interval(mins => b.buffer_before_minutes),b.end_at + make_interval(mins => b.buffer_after_minutes),'[)') && tstzrange($3,$4,'[)')) OR EXISTS(SELECT 1 FROM cafe.cafe_booking_holds h WHERE h.workspace_id=$1 AND h.booth_id=$2 AND h.status='active' AND h.expires_at>now() AND tstzrange(h.start_at,h.end_at,'[)') && tstzrange($3,$4,'[)')) OR EXISTS(SELECT 1 FROM cafe.cafe_booth_blackouts x WHERE x.workspace_id=$1 AND (x.booth_id=$2 OR x.booth_id IS NULL) AND tstzrange(x.start_at,x.end_at,'[)') && tstzrange($3,$4,'[)'))`, workspace, q.BoothID, occupiedStart, occupiedEnd).Scan(&blocked)
		if err != nil {
			return nil, MapError(err)
		}
		if !blocked && cursor.After(minimum) && cursor.Before(maximum) {
			slots = append(slots, domain.AvailabilitySlot{Start: cursor.UTC(), End: slotEnd.UTC()})
		}
	}
	return slots, nil
}

func (s *Store) Schedules(ctx context.Context, workspace, boothID string) ([]domain.OperatingSchedule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,coalesce(booth_id::text,''),weekday,to_char(open_time,'HH24:MI'),to_char(close_time,'HH24:MI'),closed,slot_increment_minutes,min_advance_minutes,max_horizon_days,buffer_before_minutes,buffer_after_minutes,timezone FROM cafe_operating_schedules WHERE workspace_id=$1 AND ($2='' OR booth_id=$2) ORDER BY weekday`, workspace, boothID)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []domain.OperatingSchedule{}
	for rows.Next() {
		var item domain.OperatingSchedule
		if err := rows.Scan(&item.ID, &item.BoothID, &item.Weekday, &item.OpenTime, &item.CloseTime, &item.Closed, &item.SlotIncrement, &item.MinAdvance, &item.MaxHorizonDays, &item.BufferBefore, &item.BufferAfter, &item.Timezone); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSchedule(ctx context.Context, workspace, actor string, input domain.OperatingSchedule) (domain.OperatingSchedule, error) {
	if input.Weekday < 0 || input.Weekday > 6 || input.OpenTime == "" || input.CloseTime == "" || input.SlotIncrement < 5 || input.SlotIncrement > 120 || input.MinAdvance < 0 || input.MaxHorizonDays < 1 || input.MaxHorizonDays > 730 {
		return domain.OperatingSchedule{}, domain.ErrInvalid
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Ho_Chi_Minh"
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return domain.OperatingSchedule{}, domain.ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return domain.OperatingSchedule{}, err
	}
	defer tx.Rollback()
	boothID := strings.TrimSpace(input.BoothID)
	var boothArg any = nil
	if boothID != "" {
		if !domain.ValidID(boothID) {
			return domain.OperatingSchedule{}, domain.ErrInvalid
		}
		boothArg = boothID
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM cafe_operating_schedules WHERE workspace_id=$1 AND weekday=$2 AND ((booth_id=$3) OR (booth_id IS NULL AND $3 IS NULL))`, workspace, input.Weekday, boothArg)
	if err != nil {
		return domain.OperatingSchedule{}, MapError(err)
	}
	var out domain.OperatingSchedule
	if input.BufferBefore < 0 || input.BufferBefore > 240 || input.BufferAfter < 0 || input.BufferAfter > 240 {
		return domain.OperatingSchedule{}, domain.ErrInvalid
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO cafe_operating_schedules(workspace_id,booth_id,weekday,open_time,close_time,closed,slot_increment_minutes,min_advance_minutes,max_horizon_days,buffer_before_minutes,buffer_after_minutes,timezone,created_by,updated_by) VALUES($1,$2,$3,$4::time,$5::time,$6,$7,$8,$9,$10,$11,$12,$13,$13) RETURNING id::text,coalesce(booth_id::text,''),weekday,to_char(open_time,'HH24:MI'),to_char(close_time,'HH24:MI'),closed,slot_increment_minutes,min_advance_minutes,max_horizon_days,buffer_before_minutes,buffer_after_minutes,timezone`, workspace, boothArg, input.Weekday, input.OpenTime, input.CloseTime, input.Closed, input.SlotIncrement, input.MinAdvance, input.MaxHorizonDays, input.BufferBefore, input.BufferAfter, input.Timezone, actor).Scan(&out.ID, &out.BoothID, &out.Weekday, &out.OpenTime, &out.CloseTime, &out.Closed, &out.SlotIncrement, &out.MinAdvance, &out.MaxHorizonDays, &out.BufferBefore, &out.BufferAfter, &out.Timezone)
	if err != nil {
		return domain.OperatingSchedule{}, MapError(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.OperatingSchedule{}, MapError(err)
	}
	return out, nil
}

func (s *Store) Blackouts(ctx context.Context, workspace, boothID string) ([]domain.Blackout, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,coalesce(booth_id::text,''),start_at,end_at,reason FROM cafe_booth_blackouts WHERE workspace_id=$1 AND ($2='' OR booth_id=$2) AND end_at>now() ORDER BY start_at`, workspace, boothID)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	out := []domain.Blackout{}
	for rows.Next() {
		var b domain.Blackout
		if err := rows.Scan(&b.ID, &b.BoothID, &b.Start, &b.End, &b.Reason); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) CreateBlackout(ctx context.Context, workspace, actor string, input domain.Blackout) (domain.Blackout, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.End.Before(input.Start) || input.End.Equal(input.Start) || input.Reason == "" || len(input.Reason) > 300 || input.Start.Before(time.Now().Add(-24*time.Hour)) {
		return domain.Blackout{}, domain.ErrInvalid
	}
	if input.BoothID != "" && !domain.ValidID(input.BoothID) {
		return domain.Blackout{}, domain.ErrInvalid
	}
	var b domain.Blackout
	err := s.DB.QueryRowContext(ctx, `INSERT INTO cafe_booth_blackouts(workspace_id,booth_id,start_at,end_at,reason,created_by) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6) RETURNING id::text,coalesce(booth_id::text,''),start_at,end_at,reason`, workspace, input.BoothID, input.Start, input.End, input.Reason, actor).Scan(&b.ID, &b.BoothID, &b.Start, &b.End, &b.Reason)
	return b, MapError(err)
}

func (s *Store) DeleteBlackout(ctx context.Context, workspace, id string) error {
	if !domain.ValidID(id) {
		return domain.ErrInvalid
	}
	result, err := s.DB.ExecContext(ctx, `DELETE FROM cafe_booth_blackouts WHERE workspace_id=$1 AND id=$2`, workspace, id)
	if err != nil {
		return MapError(err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) Utilization(ctx context.Context, workspace string, from, to time.Time) (any, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT booth_id::text,count(*) FILTER(WHERE status NOT IN ('cancelled','no_show'))::int,count(*) FILTER(WHERE status='completed')::int,coalesce(sum(EXTRACT(EPOCH FROM (end_at-start_at))/60) FILTER(WHERE status NOT IN ('cancelled','no_show')),0)::int FROM cafe_bookings WHERE workspace_id=$1 AND start_at<$3 AND end_at>$2 GROUP BY booth_id ORDER BY booth_id`, workspace, from, to)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()
	type metric struct {
		BoothID   string `json:"booth_id"`
		Bookings  int    `json:"bookings"`
		Completed int    `json:"completed"`
		Minutes   int    `json:"booked_minutes"`
	}
	out := []metric{}
	for rows.Next() {
		var m metric
		if err := rows.Scan(&m.BoothID, &m.Bookings, &m.Completed, &m.Minutes); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return map[string]any{"from": from, "to": to, "booths": out}, rows.Err()
}

func ReadSchema(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	return string(bytes), err
}
