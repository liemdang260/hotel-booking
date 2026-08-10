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

type paymentClient struct{seen context.Context;response *PaymentResponse;err error}
func(c *paymentClient)CreatePayment(ctx context.Context,*CreatePaymentRequest)(*PaymentResponse,error){c.seen=ctx;return c.response,c.err}
func(c *paymentClient)GetPayment(ctx context.Context,*GetPaymentRequest)(*PaymentResponse,error){c.seen=ctx;return c.response,c.err}
func TestPaymentAdapterPropagatesCallerContext(t *testing.T){
	type key string
	ctx,cancel:=context.WithTimeout(context.WithValue(context.Background(),key("trace"),"t-1"),time.Second);defer cancel()
	client:=&paymentClient{response:&PaymentResponse{PaymentID:"p1",BookingID:"b1",Status:"SUCCEEDED"}}
	_,err:=NewPaymentAdapter(client).CreatePayment(ctx,repository.CreatePaymentCommand{})
	if err!=nil{t.Fatal(err)}
	if client.seen!=ctx||client.seen.Value(key("trace"))!="t-1"{t.Fatal("caller context was replaced")}
	if _,ok:=client.seen.Deadline();!ok{t.Fatal("deadline was not propagated")}
}
func TestMutationDeadlineIsOutcomeUnknown(t *testing.T){
	client:=&paymentClient{err:status.Error(codes.DeadlineExceeded,"provider result not observed")}
	_,err:=NewPaymentAdapter(client).CreatePayment(context.Background(),repository.CreatePaymentCommand{})
	if !errors.Is(err,repository.ErrOutcomeUnknown){t.Fatalf("got %v",err)}
}
func TestStructuredReasonPreserved(t *testing.T){
	st:=status.New(codes.FailedPrecondition,"quote expired")
	st,err:=st.WithDetails(&errdetails.ErrorInfo{Reason:"QUOTE_EXPIRED",Domain:"pricing.hotelbooking"})
	if err!=nil{t.Fatal(err)}
	mapped:=mapRPCError(context.Background(),st.Err(),false)
	if !errors.Is(mapped,repository.ErrQuoteExpired){t.Fatalf("got %v",mapped)}
}
func TestReadUnavailableIsNotMutationUnknown(t *testing.T){
	client:=&paymentClient{err:status.Error(codes.Unavailable,"offline")}
	_,err:=NewPaymentAdapter(client).GetPayment(context.Background(),"p1")
	if !errors.Is(err,repository.ErrDownstreamUnavailable)||errors.Is(err,repository.ErrOutcomeUnknown){t.Fatalf("got %v",err)}
}
