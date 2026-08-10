package grpcadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
)
type AvailabilityClient interface {
	ReserveInventory(context.Context,*ReserveInventoryRequest)(*ReservationResponse,error)
	GetReservation(context.Context,*GetReservationRequest)(*ReservationResponse,error)
	ConfirmReservation(context.Context,*ReservationCommand)(*ReservationResponse,error)
	ReleaseReservation(context.Context,*ReleaseReservationRequest)(*ReservationResponse,error)
}
type ReserveInventoryRequest struct{BookingID,HotelID,RoomTypeID string;CheckIn,CheckOut time.Time;Quantity int}
type GetReservationRequest struct{ReservationID string}
type ReservationCommand struct{ReservationID string}
type ReleaseReservationRequest struct{ReservationID,Reason string}
type ReservationResponse struct{ReservationID,BookingID,Status string;ExpiresAt time.Time}
type AvailabilityAdapter struct{client AvailabilityClient}
func NewAvailabilityAdapter(c AvailabilityClient)*AvailabilityAdapter{return &AvailabilityAdapter{client:c}}
func(a *AvailabilityAdapter)ReserveInventory(ctx context.Context,c repository.ReserveInventoryCommand)(repository.Reservation,error){
	r,e:=a.client.ReserveInventory(ctx,&ReserveInventoryRequest{c.BookingID,c.HotelID,c.RoomTypeID,c.CheckIn,c.CheckOut,c.Quantity})
	return mapReservation(ctx,r,e,true)
}
func(a *AvailabilityAdapter)GetReservation(ctx context.Context,id string)(repository.Reservation,error){
	r,e:=a.client.GetReservation(ctx,&GetReservationRequest{id});return mapReservation(ctx,r,e,false)
}
func(a *AvailabilityAdapter)ConfirmReservation(ctx context.Context,id string)(repository.Reservation,error){
	r,e:=a.client.ConfirmReservation(ctx,&ReservationCommand{id});return mapReservation(ctx,r,e,true)
}
func(a *AvailabilityAdapter)ReleaseReservation(ctx context.Context,id,reason string)(repository.Reservation,error){
	r,e:=a.client.ReleaseReservation(ctx,&ReleaseReservationRequest{id,reason});return mapReservation(ctx,r,e,true)
}
func mapReservation(ctx context.Context,r *ReservationResponse,e error,mutation bool)(repository.Reservation,error){
	if e!=nil{return repository.Reservation{},mapRPCError(ctx,e,mutation)}
	if r==nil||r.ReservationID==""{return repository.Reservation{},fmt.Errorf("%w: malformed reservation",repository.ErrInvalidRemoteResponse)}
	status:=repository.ReservationStatus(r.Status)
	switch status{case repository.ReservationHeld,repository.ReservationBooked,repository.ReservationReleased,repository.ReservationExpiredStatus:
	default:return repository.Reservation{},fmt.Errorf("%w: reservation status %q",repository.ErrInvalidRemoteResponse,r.Status)}
	return repository.Reservation{ID:r.ReservationID,BookingID:r.BookingID,Status:status,ExpiresAt:r.ExpiresAt},nil
}
