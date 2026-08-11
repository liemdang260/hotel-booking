package domain

import (
	"errors"
	"math"
	"time"
)

var (
	ErrInvalidStay               = errors.New("invalid stay date range")
	ErrInvalidParty              = errors.New("room quantity and guest count must be positive")
	ErrInvalidMoney              = errors.New("invalid monetary value")
	ErrQuoteExpired              = errors.New("quote expired")
	ErrInvalidCancellationPolicy = errors.New("invalid cancellation policy")
)

type Date struct {
	Year  int
	Month time.Month
	Day   int
}

func NewDate(year int, month time.Month, day int) (Date, error) {
	value := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if value.Year() != year || value.Month() != month || value.Day() != day {
		return Date{}, ErrInvalidStay
	}
	return Date{Year: year, Month: month, Day: day}, nil
}

func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

func (d Date) DaysUntil(other Date) (int, error) {
	days := int(other.Time().Sub(d.Time()) / (24 * time.Hour))
	if days < 1 {
		return 0, ErrInvalidStay
	}
	return days, nil
}

type QuoteInput struct {
	HotelID      string
	RoomTypeID   string
	CheckIn      Date
	CheckOut     Date
	GuestCount   int
	RoomQuantity int
}

func (i QuoteInput) Validate() error {
	if i.HotelID == "" || i.RoomTypeID == "" {
		return ErrInvalidStay
	}
	if i.GuestCount < 1 || i.RoomQuantity < 1 {
		return ErrInvalidParty
	}
	_, err := i.CheckIn.DaysUntil(i.CheckOut)
	return err
}

type CancellationPolicyRule struct {
	PolicyCode                  string
	PolicyVersion               string
	HotelTimeZone               string
	FreeCancelDaysBeforeCheckIn int
	FreeCancelLocalHour         int
	RefundBasisPoints           int
	CancellationFeeMinor        int64
}

type CancellationPolicy struct {
	PolicyCode           string
	PolicyVersion        string
	FreeCancelUntil      time.Time
	RefundBasisPoints    int
	CancellationFeeMinor int64
}

func ResolveCancellationPolicy(checkIn Date, rule CancellationPolicyRule) (CancellationPolicy, error) {
	if rule.PolicyCode == "" || rule.PolicyVersion == "" || rule.HotelTimeZone == "" ||
		rule.FreeCancelDaysBeforeCheckIn < 0 || rule.FreeCancelLocalHour < 0 || rule.FreeCancelLocalHour > 23 ||
		rule.RefundBasisPoints < 0 || rule.RefundBasisPoints > 10000 || rule.CancellationFeeMinor < 0 {
		return CancellationPolicy{}, ErrInvalidCancellationPolicy
	}
	location, err := time.LoadLocation(rule.HotelTimeZone)
	if err != nil {
		return CancellationPolicy{}, ErrInvalidCancellationPolicy
	}
	localCheckIn := time.Date(checkIn.Year, checkIn.Month, checkIn.Day, 0, 0, 0, 0, location)
	deadlineDay := localCheckIn.AddDate(0, 0, -rule.FreeCancelDaysBeforeCheckIn)
	deadline := time.Date(deadlineDay.Year(), deadlineDay.Month(), deadlineDay.Day(), rule.FreeCancelLocalHour, 0, 0, 0, location).UTC()
	return CancellationPolicy{
		PolicyCode:           rule.PolicyCode,
		PolicyVersion:        rule.PolicyVersion,
		FreeCancelUntil:      deadline,
		RefundBasisPoints:    rule.RefundBasisPoints,
		CancellationFeeMinor: rule.CancellationFeeMinor,
	}, nil
}

type RatePlan struct {
	PricingVersion   string
	Currency         string
	NightlyMinor     int64
	TaxBasisPoints   int64
	ServiceFeeMinor  int64
	DiscountMinor    int64
	CancellationRule CancellationPolicyRule
}

type PriceBreakdown struct {
	SubtotalMinor   int64
	TaxMinor        int64
	ServiceFeeMinor int64
	DiscountMinor   int64
	TotalMinor      int64
}

func CalculatePrice(input QuoteInput, plan RatePlan) (PriceBreakdown, error) {
	if err := input.Validate(); err != nil {
		return PriceBreakdown{}, err
	}
	if plan.PricingVersion == "" || plan.Currency == "" || plan.NightlyMinor < 0 ||
		plan.TaxBasisPoints < 0 || plan.ServiceFeeMinor < 0 || plan.DiscountMinor < 0 {
		return PriceBreakdown{}, ErrInvalidMoney
	}
	nights, _ := input.CheckIn.DaysUntil(input.CheckOut)
	roomNights, ok := checkedMultiply(int64(nights), int64(input.RoomQuantity))
	if !ok {
		return PriceBreakdown{}, ErrInvalidMoney
	}
	subtotal, ok := checkedMultiply(plan.NightlyMinor, roomNights)
	if !ok {
		return PriceBreakdown{}, ErrInvalidMoney
	}
	taxProduct, ok := checkedMultiply(subtotal, plan.TaxBasisPoints)
	if !ok {
		return PriceBreakdown{}, ErrInvalidMoney
	}
	tax := taxProduct / 10000
	beforeDiscount, ok := checkedAdd(subtotal, tax)
	if !ok {
		return PriceBreakdown{}, ErrInvalidMoney
	}
	beforeDiscount, ok = checkedAdd(beforeDiscount, plan.ServiceFeeMinor)
	if !ok || plan.DiscountMinor > beforeDiscount {
		return PriceBreakdown{}, ErrInvalidMoney
	}
	return PriceBreakdown{
		SubtotalMinor:   subtotal,
		TaxMinor:        tax,
		ServiceFeeMinor: plan.ServiceFeeMinor,
		DiscountMinor:   plan.DiscountMinor,
		TotalMinor:      beforeDiscount - plan.DiscountMinor,
	}, nil
}

func checkedMultiply(left, right int64) (int64, bool) {
	if left < 0 || right < 0 {
		return 0, false
	}
	if left != 0 && right > math.MaxInt64/left {
		return 0, false
	}
	return left * right, true
}

func checkedAdd(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || right > math.MaxInt64-left {
		return 0, false
	}
	return left + right, true
}

type Quote struct {
	ID                 string
	Input              QuoteInput
	Price              PriceBreakdown
	Currency           string
	PricingVersion     string
	CancellationPolicy CancellationPolicy
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

func (q Quote) IsExpired(now time.Time) bool {
	return !now.Before(q.ExpiresAt)
}
