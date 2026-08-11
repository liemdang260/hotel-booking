package grpcadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/booking/internal/repository"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type paymentClient struct { seen context.Context; response *PaymentResponse; err error }
func(c *paymentClient)CreatePayment(ctx context.Context,_ *CreatePaymentRequest)(*PaymentResponse,error){c.seen=ctx;return c.response,c.err}
func(c *paymentClient)GetPayment(ctx context.Context,_ *GetPaymentRequest)(*PaymentResponse,error){c.seen=ctx;return c.response,c.err}

type pricingClient struct{response *GetQuoteResponse;err error}
func(c *pricingClient)GetQuote(context.Context,*GetQuoteRequest)(*GetQuoteResponse,error){return c.response,c.err}

func TestPricingAdapterPreservesAcceptedCancellationTerms(t *testing.T){
	deadline:=time.Date(2026,8,30,11,0,0,0,time.UTC)
	client:=&pricingClient{response:&GetQuoteResponse{QuoteID:"q1",HotelID:"h1",RoomTypeID:"r1",Currency:"USD",PricingVersion:"v1",CheckIn:time.Date(2026,9,1,0,0,0,0,time.UTC),CheckOut:time.Date(2026,9,4,0,0,0,0,time.UTC),GuestCount:2,RoomQuantity:1,TotalMinor:10000,CancellationPolicy:CancellationPolicyResponse{PolicyCode:"FLEXIBLE",PolicyVersion:"policy-v1",FreeCancelUntil:deadline,RefundBasisPoints:10000,CancellationFeeMinor:0},CreatedAt:time.Now().UTC(),ExpiresAt:time.Now().UTC().Add(time.Minute)}}
	quote,err:=NewPricingAdapter(client).GetQuote(context.Background(),"q1");if err!=nil{t.Fatal(err)}
	if quote.CancellationPolicy.PolicyCode!="FLEXIBLE"||quote.CancellationPolicy.PolicyVersion!="policy-v1"||!quote.CancellationPolicy.FreeCancelUntil.Equal(deadline)||quote.CancellationPolicy.RefundBasisPoints!=10000{t.Fatalf("policy=%+v",quote.CancellationPolicy)}
}

func TestPricingAdapterRejectsMissingCancellationTerms(t *testing.T){
	client:=&pricingClient{response:&GetQuoteResponse{QuoteID:"q1",Currency:"USD",TotalMinor:10000}}
	_,err:=NewPricingAdapter(client).GetQuote(context.Background(),"q1");if !errors.Is(err,repository.ErrInvalidRemoteResponse){t.Fatalf("got %v",err)}
}

func TestPaymentAdapterPropagatesCallerContext(t *testing.T){type key string;ctx,cancel:=context.WithTimeout(context.WithValue(context.Background(),key("trace"),"t-1"),time.Second);defer cancel();client:=&paymentClient{response:&PaymentResponse{PaymentID:"p1",BookingID:"b1",Status:"SUCCEEDED"}};_,err:=NewPaymentAdapter(client).CreatePayment(ctx,repository.CreatePaymentCommand{});if err!=nil{t.Fatal(err)};if client.seen!=ctx||client.seen.Value(key("trace"))!="t-1"{t.Fatal("caller context was replaced")};if _,ok:=client.seen.Deadline();!ok{t.Fatal("deadline was not propagated")}}
func TestMutationDeadlineIsOutcomeUnknown(t *testing.T){client:=&paymentClient{err:status.Error(codes.DeadlineExceeded,"provider result not observed")};_,err:=NewPaymentAdapter(client).CreatePayment(context.Background(),repository.CreatePaymentCommand{});if !errors.Is(err,repository.ErrOutcomeUnknown){t.Fatalf("got %v",err)}}
func TestStructuredReasonPreserved(t *testing.T){st:=status.New(codes.FailedPrecondition,"quote expired");st,err:=st.WithDetails(&errdetails.ErrorInfo{Reason:"QUOTE_EXPIRED",Domain:"pricing.hotelbooking"});if err!=nil{t.Fatal(err)};mapped:=mapRPCError(context.Background(),st.Err(),false);if !errors.Is(mapped,repository.ErrQuoteExpired){t.Fatalf("got %v",mapped)}}
func TestReadUnavailableIsNotMutationUnknown(t *testing.T){client:=&paymentClient{err:status.Error(codes.Unavailable,"offline")};_,err:=NewPaymentAdapter(client).GetPayment(context.Background(),"p1");if !errors.Is(err,repository.ErrDownstreamUnavailable)||errors.Is(err,repository.ErrOutcomeUnknown){t.Fatalf("got %v",err)}}
