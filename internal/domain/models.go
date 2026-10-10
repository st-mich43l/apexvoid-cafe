package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid input")
	ErrConflict = errors.New("resource conflict")
	ErrNotFound = errors.New("resource not found")
	ErrTimezoneUnavailable = errors.New("timezone database is unavailable")
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func ValidID(id string) bool { return uuidPattern.MatchString(id) }

type Item struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	SKU             string `json:"sku"`
	Kind            string `json:"kind"`
	PriceVND        int64  `json:"price_vnd"`
	DurationMinutes int    `json:"duration_minutes"`
	Active          bool   `json:"active"`
}

type ItemInput struct {
	Name            string `json:"name"`
	SKU             string `json:"sku"`
	Kind            string `json:"kind"`
	PriceVND        int64  `json:"price_vnd"`
	DurationMinutes int    `json:"duration_minutes"`
}

func (in ItemInput) Normalize() (ItemInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.SKU = strings.ToUpper(strings.TrimSpace(in.SKU))
	if len(in.Name) < 1 || len(in.Name) > 160 || len(in.SKU) < 1 || len(in.SKU) > 64 ||
		(in.Kind != "drink" && in.Kind != "photo") || in.PriceVND < 0 || in.PriceVND > 100_000_000 || in.DurationMinutes < 0 || in.DurationMinutes > 480 {
		return ItemInput{}, ErrInvalid
	}
	if in.DurationMinutes == 0 {
		in.DurationMinutes = 20
	}
	for _, c := range in.SKU {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return ItemInput{}, ErrInvalid
		}
	}
	return in, nil
}

type Booth struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type OrderLineInput struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}
type OrderInput struct {
	Note  string           `json:"note"`
	Lines []OrderLineInput `json:"lines"`
}

func (in OrderInput) Validate() (OrderInput, error) {
	in.Note = strings.TrimSpace(in.Note)
	if len(in.Note) > 300 || len(in.Lines) == 0 || len(in.Lines) > 30 {
		return OrderInput{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, line := range in.Lines {
		if !ValidID(line.ItemID) || line.Quantity < 1 || line.Quantity > 99 || seen[line.ItemID] {
			return OrderInput{}, ErrInvalid
		}
		seen[line.ItemID] = true
	}
	return in, nil
}

type Order struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	TotalVND  int64     `json:"total_vnd"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type BookingInput struct {
	BoothID        string    `json:"booth_id"`
	PackageID      string    `json:"package_id"`
	GuestName      string    `json:"guest_name"`
	GuestPhone     string    `json:"guest_phone"`
	GuestEmail     string    `json:"guest_email"`
	PartySize      int       `json:"party_size"`
	Notes          string    `json:"notes"`
	Addons         []string  `json:"addons"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	BufferBefore   int       `json:"buffer_before_minutes"`
	BufferAfter    int       `json:"buffer_after_minutes"`
	IdempotencyKey string    `json:"idempotency_key"`
}

func (in BookingInput) Validate(now time.Time) (BookingInput, error) {
	return in.ValidateWithHorizon(now, 90*24*time.Hour)
}

func (in BookingInput) ValidateWithHorizon(now time.Time, horizon time.Duration) (BookingInput, error) {
	in.GuestName = strings.TrimSpace(in.GuestName)
	in.GuestPhone = strings.TrimSpace(in.GuestPhone)
	in.GuestEmail = strings.TrimSpace(in.GuestEmail)
	in.Notes = strings.TrimSpace(in.Notes)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.PartySize == 0 {
		in.PartySize = 1
	}
	if in.BufferBefore < 0 || in.BufferAfter < 0 || in.BufferBefore > 240 || in.BufferAfter > 240 {
		return BookingInput{}, ErrInvalid
	}
	dur := in.End.Sub(in.Start)
	if !ValidID(in.BoothID) || !ValidID(in.PackageID) || len(in.GuestName) < 1 || len(in.GuestName) > 120 ||
		len(in.GuestPhone) > 40 || len(in.GuestEmail) > 254 || len(in.Notes) > 1000 || len(in.Addons) > 20 || len(in.IdempotencyKey) > 128 ||
		in.PartySize < 1 || in.PartySize > 100 || in.Start.Before(now.Add(-5*time.Minute)) || in.Start.After(now.Add(horizon)) ||
		dur < 5*time.Minute || dur > 8*time.Hour {
		return BookingInput{}, ErrInvalid
	}
	return in, nil
}

const (
	BookingConfirmed  = "confirmed"
	BookingCheckedIn  = "checked_in"
	BookingInProgress = "in_progress"
	BookingCompleted  = "completed"
	BookingCancelled  = "cancelled"
	BookingNoShow     = "no_show"
)

func ValidBookingStatus(value string) bool {
	switch value {
	case BookingConfirmed, BookingCheckedIn, BookingInProgress, BookingCompleted, BookingCancelled, BookingNoShow:
		return true
	default:
		return false
	}
}

func CanTransitionBooking(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case BookingConfirmed:
		return to == BookingCheckedIn || to == BookingCancelled || to == BookingNoShow
	case BookingCheckedIn:
		return to == BookingInProgress || to == BookingCancelled
	case BookingInProgress:
		return to == BookingCompleted
	default:
		return false
	}
}

type BookingFilter struct {
	From     time.Time
	To       time.Time
	BoothID  string
	Status   string
	Guest    string
	Page     int
	PageSize int
}

func (f BookingFilter) Normalize() (BookingFilter, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	f.Guest = strings.TrimSpace(f.Guest)
	if f.BoothID != "" && !ValidID(f.BoothID) {
		return BookingFilter{}, ErrInvalid
	}
	if f.Status != "" && !ValidBookingStatus(f.Status) {
		return BookingFilter{}, ErrInvalid
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.To.After(f.From) {
		return BookingFilter{}, ErrInvalid
	}
	return f, nil
}

type Page[T any] struct {
	Items    []T  `json:"items"`
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	HasMore  bool `json:"has_more"`
}

type Booking struct {
	ID                 string     `json:"id"`
	BookingRef         string     `json:"booking_ref"`
	WorkspaceID        string     `json:"workspace_id,omitempty"`
	BoothID            string     `json:"booth_id"`
	BoothName          string     `json:"booth_name"`
	PackageID          string     `json:"package_id"`
	GuestName          string     `json:"guest_name"`
	GuestPhone         string     `json:"guest_phone,omitempty"`
	GuestEmail         string     `json:"guest_email,omitempty"`
	PackageName        string     `json:"package_name"`
	Start              time.Time  `json:"start"`
	End                time.Time  `json:"end"`
	Status             string     `json:"status"`
	PriceVND           int64      `json:"price_vnd"`
	PartySize          int        `json:"party_size"`
	Notes              string     `json:"notes,omitempty"`
	Addons             []string   `json:"addons,omitempty"`
	BufferBefore       int        `json:"buffer_before_minutes"`
	BufferAfter        int        `json:"buffer_after_minutes"`
	ActualCheckedInAt  *time.Time `json:"actual_checked_in_at,omitempty"`
	SessionStartedAt   *time.Time `json:"session_started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	CancelledAt        *time.Time `json:"cancelled_at,omitempty"`
	CancellationReason string     `json:"cancellation_reason,omitempty"`
	NoShowAt           *time.Time `json:"no_show_at,omitempty"`
	NoShowReason       string     `json:"no_show_reason,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type BookingEvent struct {
	ID        string         `json:"id"`
	BookingID string         `json:"booking_id"`
	ActorID   string         `json:"actor_id"`
	EventType string         `json:"event_type"`
	From      string         `json:"from_status,omitempty"`
	To        string         `json:"to_status,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Changes   map[string]any `json:"changes,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type AvailabilityQuery struct {
	BoothID   string    `json:"booth_id"`
	PackageID string    `json:"package_id"`
	Date      time.Time `json:"date"`
}

type AvailabilitySlot struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Hold struct {
	ID        string    `json:"id"`
	BoothID   string    `json:"booth_id"`
	PackageID string    `json:"package_id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type HoldInput struct {
	BoothID    string    `json:"booth_id"`
	PackageID  string    `json:"package_id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	TTLSeconds int       `json:"ttl_seconds"`
}

func (in HoldInput) Validate(now time.Time) (HoldInput, error) {
	if !ValidID(in.BoothID) || !ValidID(in.PackageID) || in.Start.Before(now.Add(-5*time.Minute)) || in.End.Sub(in.Start) < 5*time.Minute || in.End.Sub(in.Start) > 8*time.Hour {
		return HoldInput{}, ErrInvalid
	}
	if in.TTLSeconds == 0 {
		in.TTLSeconds = 600
	}
	if in.TTLSeconds < 30 || in.TTLSeconds > 1800 {
		return HoldInput{}, ErrInvalid
	}
	return in, nil
}

type OperatingSchedule struct {
	ID             string `json:"id"`
	BoothID        string `json:"booth_id,omitempty"`
	Weekday        int    `json:"weekday"`
	OpenTime       string `json:"open_time"`
	CloseTime      string `json:"close_time"`
	Closed         bool   `json:"closed"`
	SlotIncrement  int    `json:"slot_increment_minutes"`
	MinAdvance     int    `json:"min_advance_minutes"`
	MaxHorizonDays int    `json:"max_horizon_days"`
	BufferBefore   int    `json:"buffer_before_minutes"`
	BufferAfter    int    `json:"buffer_after_minutes"`
	Timezone       string `json:"timezone"`
}

type ScheduleException struct {
	ID        string    `json:"id"`
	Date      time.Time `json:"date"`
	Closed    bool      `json:"closed"`
	OpenTime  string    `json:"open_time"`
	CloseTime string    `json:"close_time"`
	Reason    string    `json:"reason"`
}

type Blackout struct {
	ID      string    `json:"id"`
	BoothID string    `json:"booth_id,omitempty"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Reason  string    `json:"reason"`
}
