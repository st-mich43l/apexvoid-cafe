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
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func ValidID(id string) bool { return uuidPattern.MatchString(id) }

type Item struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SKU      string `json:"sku"`
	Kind     string `json:"kind"`
	PriceVND int64  `json:"price_vnd"`
	Active   bool   `json:"active"`
}

type ItemInput struct {
	Name     string `json:"name"`
	SKU      string `json:"sku"`
	Kind     string `json:"kind"`
	PriceVND int64  `json:"price_vnd"`
}

func (in ItemInput) Normalize() (ItemInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.SKU = strings.ToUpper(strings.TrimSpace(in.SKU))
	if len(in.Name) < 1 || len(in.Name) > 160 || len(in.SKU) < 1 || len(in.SKU) > 64 ||
		(in.Kind != "drink" && in.Kind != "photo") || in.PriceVND < 0 || in.PriceVND > 100_000_000 {
		return ItemInput{}, ErrInvalid
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
	BoothID   string    `json:"booth_id"`
	PackageID string    `json:"package_id"`
	GuestName string    `json:"guest_name"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
}

func (in BookingInput) Validate(now time.Time) (BookingInput, error) {
	in.GuestName = strings.TrimSpace(in.GuestName)
	dur := in.End.Sub(in.Start)
	if !ValidID(in.BoothID) || !ValidID(in.PackageID) || len(in.GuestName) < 1 || len(in.GuestName) > 120 ||
		in.Start.Before(now.Add(-5*time.Minute)) || in.Start.After(now.AddDate(0, 3, 0)) ||
		dur < 10*time.Minute || dur > 2*time.Hour {
		return BookingInput{}, ErrInvalid
	}
	return in, nil
}

type Booking struct {
	ID          string    `json:"id"`
	BoothID     string    `json:"booth_id"`
	BoothName   string    `json:"booth_name"`
	GuestName   string    `json:"guest_name"`
	PackageName string    `json:"package_name"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Status      string    `json:"status"`
	PriceVND    int64     `json:"price_vnd"`
}
