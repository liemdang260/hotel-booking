package usecase

import (
	"context"
	"errors"
	"testing"
)

func TestBatchCheckAvailabilityReturnsAllItemsInRequestOrder(t *testing.T) {
	service, _, _, base := fixture(t)
	items := []CheckAvailabilityInput{
		{HotelID: base.HotelID, RoomTypeID: base.RoomTypeID, CheckIn: base.CheckIn, CheckOut: base.CheckOut, Quantity: 1},
		{HotelID: base.HotelID, RoomTypeID: base.RoomTypeID, CheckIn: base.CheckIn, CheckOut: base.CheckOut, Quantity: 3},
	}
	results, err := service.BatchCheckAvailability(context.Background(), BatchCheckAvailabilityInput{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !results[0].Available || !results[1].Available || results[0].AvailableQuantity != 3 {
		t.Fatalf("results=%+v", results)
	}
}

func TestBatchCheckAvailabilityRejectsUnboundedInput(t *testing.T) {
	service, _, _, base := fixture(t)
	items := make([]CheckAvailabilityInput, maxBatchAvailabilityItems+1)
	for i := range items {
		items[i] = CheckAvailabilityInput{
			HotelID: base.HotelID,
			RoomTypeID: base.RoomTypeID,
			CheckIn: base.CheckIn,
			CheckOut: base.CheckOut,
			Quantity: 1,
		}
	}
	_, err := service.BatchCheckAvailability(context.Background(), BatchCheckAvailabilityInput{Items: items})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}
