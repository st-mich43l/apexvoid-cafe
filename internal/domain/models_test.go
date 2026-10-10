package domain

import (
	"errors"
	"testing"
	"time"
)

const sampleID = "123e4567-e89b-42d3-a456-426614174000"

func TestCatalog(t *testing.T) {
	valid, err := (ItemInput{Name: " Iced Latte ", SKU: " coffee-01 ", Kind: "drink", PriceVND: 49000}).Normalize()
	if err != nil || valid.SKU != "COFFEE-01" || valid.Name != "Iced Latte" {
		t.Fatalf("unexpected item: %#v %v", valid, err)
	}
	_, err = (ItemInput{Name: "x", SKU: "a/b", Kind: "photo", PriceVND: 5000}).Normalize()
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("unsafe SKU accepted")
	}
}
func TestOrder(t *testing.T) {
	if _, err := (OrderInput{Lines: []OrderLineInput{{ItemID: sampleID, Quantity: 1}, {ItemID: sampleID, Quantity: 2}}}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate line accepted")
	}
}
func TestBooking(t *testing.T) {
	now := time.Now().UTC()
	b := BookingInput{BoothID: sampleID, PackageID: sampleID, GuestName: " Visitor ", Start: now.Add(time.Hour), End: now.Add(80 * time.Minute)}
	got, err := b.Validate(now)
	if err != nil || got.GuestName != "Visitor" {
		t.Fatalf("unexpected booking %#v %v", got, err)
	}
	b.End = b.Start.Add(-time.Minute)
	if _, err := b.Validate(now); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative duration accepted")
	}
}

func TestBookingLifecycle(t *testing.T) {
	valid := [][2]string{
		{BookingConfirmed, BookingCheckedIn},
		{BookingCheckedIn, BookingInProgress},
		{BookingInProgress, BookingCompleted},
		{BookingConfirmed, BookingCancelled},
		{BookingConfirmed, BookingNoShow},
	}
	for _, transition := range valid {
		if !CanTransitionBooking(transition[0], transition[1]) {
			t.Fatalf("expected transition %s -> %s", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]string{{BookingCompleted, BookingCancelled}, {BookingCancelled, BookingConfirmed}, {BookingConfirmed, BookingInProgress}, {BookingNoShow, BookingCompleted}} {
		if CanTransitionBooking(transition[0], transition[1]) {
			t.Fatalf("unexpected transition %s -> %s", transition[0], transition[1])
		}
	}
}

func TestHoldValidation(t *testing.T) {
	now := time.Now().UTC()
	input, err := (HoldInput{BoothID: sampleID, PackageID: sampleID, Start: now.Add(time.Hour), End: now.Add(90 * time.Minute)}).Validate(now)
	if err != nil || input.TTLSeconds != 600 {
		t.Fatalf("unexpected hold validation: %#v %v", input, err)
	}
	if _, err := (HoldInput{BoothID: sampleID, PackageID: sampleID, Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), TTLSeconds: 5}).Validate(now); !errors.Is(err, ErrInvalid) {
		t.Fatal("unsafe hold duration accepted")
	}
}
