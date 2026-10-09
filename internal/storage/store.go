package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
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
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,name,sku,kind,price_vnd,active FROM cafe_items WHERE workspace_id=$1 ORDER BY kind,name LIMIT 200`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Item{}
	for rows.Next() {
		var x domain.Item
		if err := rows.Scan(&x.ID, &x.Name, &x.SKU, &x.Kind, &x.PriceVND, &x.Active); err != nil {
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
	err = s.DB.QueryRowContext(ctx, `INSERT INTO cafe_items(workspace_id,name,sku,kind,price_vnd,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text,name,sku,kind,price_vnd,active`, workspace, input.Name, input.SKU, input.Kind, input.PriceVND, actor).Scan(&x.ID, &x.Name, &x.SKU, &x.Kind, &x.PriceVND, &x.Active)
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

const bookingFields = `b.id::text,b.booth_id::text,booth.name,b.guest_name,b.package_name,b.start_at,b.end_at,b.status,b.price_vnd`

func scanBooking(row *sql.Row) (domain.Booking, error) {
	var x domain.Booking
	err := row.Scan(&x.ID, &x.BoothID, &x.BoothName, &x.GuestName, &x.PackageName, &x.Start, &x.End, &x.Status, &x.PriceVND)
	return x, MapError(err)
}
func (s *Store) Bookings(ctx context.Context, workspace string) ([]domain.Booking, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+bookingFields+` FROM cafe_bookings b JOIN cafe_booths booth ON booth.id=b.booth_id AND booth.workspace_id=b.workspace_id WHERE b.workspace_id=$1 ORDER BY start_at DESC LIMIT 100`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Booking{}
	for rows.Next() {
		var x domain.Booking
		if err := rows.Scan(&x.ID, &x.BoothID, &x.BoothName, &x.GuestName, &x.PackageName, &x.Start, &x.End, &x.Status, &x.PriceVND); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) GetBooking(ctx context.Context, workspace, id string) (domain.Booking, error) {
	return scanBooking(s.DB.QueryRowContext(ctx, `SELECT `+bookingFields+` FROM cafe_bookings b JOIN cafe_booths booth ON booth.id=b.booth_id AND booth.workspace_id=b.workspace_id WHERE b.workspace_id=$1 AND b.id=$2`, workspace, id))
}
func (s *Store) Reserve(ctx context.Context, workspace, actor string, input domain.BookingInput) (domain.Booking, error) {
	input, err := input.Validate(time.Now().UTC())
	if err != nil {
		return domain.Booking{}, err
	}
	var id string
	err = s.DB.QueryRowContext(ctx, `INSERT INTO cafe_bookings(workspace_id,booth_id,package_id,guest_name,start_at,end_at,package_name,price_vnd,created_by)
 SELECT $1,booth.id,item.id,$4,$5,$6,item.name,item.price_vnd,$7
 FROM cafe_booths booth JOIN cafe_items item ON item.workspace_id=booth.workspace_id
 WHERE booth.workspace_id=$1 AND booth.id=$2 AND booth.active AND item.id=$3 AND item.active AND item.kind='photo'
 RETURNING id::text`, workspace, input.BoothID, input.PackageID, input.GuestName, input.Start, input.End, actor).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrInvalid
	}
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	return s.GetBooking(ctx, workspace, id)
}
func (s *Store) TransitionBooking(ctx context.Context, workspace, id, action string) (domain.Booking, error) {
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
	// Physical check-in only shortly before the reserved session begins.
	var changed string
	err := s.DB.QueryRowContext(ctx, `UPDATE cafe_bookings SET status=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND status=$4 AND ($5::boolean=FALSE OR (start_at <= now()+interval '10 minutes' AND end_at>now())) RETURNING id::text`, workspace, id, to, from, action == "check-in").Scan(&changed)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Booking{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Booking{}, MapError(err)
	}
	return s.GetBooking(ctx, workspace, id)
}

func ReadSchema(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	return string(bytes), err
}
