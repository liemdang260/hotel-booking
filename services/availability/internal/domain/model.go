package domain

import "time"

type HotelID string
type RoomTypeID string
type BookingID string
type ReservationID string
type EventID string

type Inventory struct {
	HotelID        HotelID
	RoomTypeID     RoomTypeID
	Date           time.Time
	TotalQuantity  int
	HeldQuantity   int
	BookedQuantity int
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (i Inventory) AvailableQuantity() int {
	return i.TotalQuantity - i.HeldQuantity - i.BookedQuantity
}

type ReservationStatus string

const (
	ReservationHeld      ReservationStatus = "HELD"
	ReservationBooked    ReservationStatus = "BOOKED"
	ReservationReleased  ReservationStatus = "RELEASED"
	ReservationExpired   ReservationStatus = "EXPIRED"
	ReservationCancelled ReservationStatus = "CANCELLED"
)

type Reservation struct {
	ID         ReservationID
	BookingID  BookingID
	HotelID    HotelID
	RoomTypeID RoomTypeID
	CheckIn    time.Time
	CheckOut   time.Time
	Quantity   int
	Status     ReservationStatus
	ExpiresAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ReservationInventory struct {
	ReservationID ReservationID
	Date          time.Time
	Quantity      int
}

type OutboxEvent struct {
	ID               EventID
	AggregateType    string
	AggregateID      string
	AggregateVersion int64
	EventType        string
	Payload          []byte
	AvailableAt      time.Time
	CreatedAt        time.Time
}
