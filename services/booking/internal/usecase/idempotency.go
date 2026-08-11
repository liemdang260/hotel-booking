package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")

type CreateBookingIdentity struct {
	UserID, QuoteID, PaymentMethodRef string
	RoomQuantity, GuestCount int
}
func (i CreateBookingIdentity) Hash() string {
	// Length-prefixing prevents ambiguous concatenation while keeping hashing
	// deterministic across process restarts.
	raw:=fmt.Sprintf("%d:%s|%d:%s|%d:%s|%d|%d",
		len(i.UserID),i.UserID,len(i.QuoteID),i.QuoteID,
		len(i.PaymentMethodRef),i.PaymentMethodRef,i.RoomQuantity,i.GuestCount)
	sum:=sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
type IdempotencyClaim struct {
	Key, RequestHash, ProposedBookingID string
}
type IdempotencyRecord struct {
	Key, RequestHash, BookingID, Status string
}
type IdempotencyStore interface {
	Claim(context.Context,IdempotencyClaim)(IdempotencyRecord,error)
}
type IdempotencyGuard struct{store IdempotencyStore}
func NewIdempotencyGuard(s IdempotencyStore)*IdempotencyGuard{return &IdempotencyGuard{store:s}}
func(g *IdempotencyGuard)BeginOrResume(ctx context.Context,key,bookingID string,in CreateBookingIdentity)(IdempotencyRecord,error){
	record,err:=g.store.Claim(ctx,IdempotencyClaim{Key:key,RequestHash:in.Hash(),ProposedBookingID:bookingID})
	if err!=nil{return IdempotencyRecord{},err}
	if record.RequestHash!=in.Hash(){return IdempotencyRecord{},ErrIdempotencyConflict}
	return record,nil
}
