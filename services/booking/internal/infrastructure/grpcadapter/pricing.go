package grpcadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
)

type PricingClient interface{GetQuote(context.Context,*GetQuoteRequest)(*GetQuoteResponse,error)}
type GetQuoteRequest struct{QuoteID string}
type GetQuoteResponse struct{
	QuoteID,HotelID,RoomTypeID,Currency,PricingVersion string
	CheckIn,CheckOut time.Time
	GuestCount,RoomQuantity int
	SubtotalMinor,TaxMinor,ServiceFeeMinor,DiscountMinor,TotalMinor int64
	CancellationPolicy CancellationPolicyResponse
	CreatedAt,ExpiresAt time.Time
}
type CancellationPolicyResponse struct{PolicyCode,PolicyVersion string;FreeCancelUntil time.Time;RefundBasisPoints int;CancellationFeeMinor int64}
type PricingAdapter struct{client PricingClient}
func NewPricingAdapter(client PricingClient)*PricingAdapter{return &PricingAdapter{client:client}}
func(a *PricingAdapter)GetQuote(ctx context.Context,id string)(repository.Quote,error){
	res,err:=a.client.GetQuote(ctx,&GetQuoteRequest{QuoteID:id});if err!=nil{return repository.Quote{},mapRPCError(ctx,err,false)}
	if res==nil||res.QuoteID==""||res.Currency==""||res.TotalMinor<0||res.CancellationPolicy.PolicyCode==""||res.CancellationPolicy.PolicyVersion==""||res.CancellationPolicy.FreeCancelUntil.IsZero()||res.CancellationPolicy.RefundBasisPoints<0||res.CancellationPolicy.RefundBasisPoints>10000||res.CancellationPolicy.CancellationFeeMinor<0{return repository.Quote{},fmt.Errorf("%w: malformed quote",repository.ErrInvalidRemoteResponse)}
	return repository.Quote{ID:res.QuoteID,HotelID:res.HotelID,RoomTypeID:res.RoomTypeID,Currency:res.Currency,PricingVersion:res.PricingVersion,CheckIn:res.CheckIn,CheckOut:res.CheckOut,GuestCount:res.GuestCount,RoomQuantity:res.RoomQuantity,SubtotalMinor:res.SubtotalMinor,TaxMinor:res.TaxMinor,ServiceFeeMinor:res.ServiceFeeMinor,DiscountMinor:res.DiscountMinor,TotalMinor:res.TotalMinor,CancellationPolicy:repository.CancellationPolicy{PolicyCode:res.CancellationPolicy.PolicyCode,PolicyVersion:res.CancellationPolicy.PolicyVersion,FreeCancelUntil:res.CancellationPolicy.FreeCancelUntil,RefundBasisPoints:res.CancellationPolicy.RefundBasisPoints,CancellationFeeMinor:res.CancellationPolicy.CancellationFeeMinor},CreatedAt:res.CreatedAt,ExpiresAt:res.ExpiresAt},nil
}
