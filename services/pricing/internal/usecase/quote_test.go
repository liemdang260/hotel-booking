package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
)

type quoteMemory struct{items map[string]domain.Quote}
func(m *quoteMemory)Insert(_ context.Context,q domain.Quote)error{if m.items==nil{m.items=map[string]domain.Quote{}};if _,ok:=m.items[q.ID];ok{return errors.New("duplicate quote")};m.items[q.ID]=q;return nil}
func(m *quoteMemory)Get(_ context.Context,id string)(domain.Quote,error){q,ok:=m.items[id];if !ok{return domain.Quote{},ErrQuoteNotFound};return q,nil}
type rateMemory struct{plan domain.RatePlan}
func(m *rateMemory)Current(context.Context,domain.QuoteInput)(domain.RatePlan,error){return m.plan,nil}
type sequenceIDs struct{next int}
func(g *sequenceIDs)NewQuoteID()(string,error){g.next++;if g.next==1{return "quote-1",nil};return "quote-2",nil}
type fixedClock struct{now time.Time}
func(c *fixedClock)Now()time.Time{return c.now}
func mustDate(t *testing.T,y int,m time.Month,d int)domain.Date{t.Helper();v,err:=domain.NewDate(y,m,d);if err!=nil{t.Fatal(err)};return v}
func quoteInput(t *testing.T)CreateQuoteInput{return CreateQuoteInput{HotelID:"hotel-1",RoomTypeID:"deluxe",CheckIn:mustDate(t,2026,time.September,1),CheckOut:mustDate(t,2026,time.September,4),GuestCount:2,RoomQuantity:2}}
func flexibleRule()domain.CancellationPolicyRule{return domain.CancellationPolicyRule{PolicyCode:"FLEXIBLE",PolicyVersion:"policy-v1",HotelTimeZone:"Asia/Ho_Chi_Minh",FreeCancelDaysBeforeCheckIn:2,FreeCancelLocalHour:18,RefundBasisPoints:10000,CancellationFeeMinor:0}}
func basePlan()domain.RatePlan{return domain.RatePlan{PricingVersion:"v1",Currency:"USD",NightlyMinor:10000,CancellationRule:flexibleRule()}}

func TestCreateQuoteUsesDateNightsAndIntegerMinorUnits(t *testing.T){
	store:=&quoteMemory{};plan:=basePlan();plan.TaxBasisPoints=1000;plan.ServiceFeeMinor=500;plan.DiscountMinor=1000
	rates:=&rateMemory{plan:plan};clock:=&fixedClock{now:time.Date(2026,8,1,23,30,0,0,time.FixedZone("client",7*60*60))}
	u,err:=NewCreateQuoteUsecase(store,rates,&sequenceIDs{},clock,5*time.Minute);if err!=nil{t.Fatal(err)}
	q,err:=u.Execute(context.Background(),quoteInput(t));if err!=nil{t.Fatal(err)}
	if q.Price.SubtotalMinor!=60000||q.Price.TaxMinor!=6000||q.Price.TotalMinor!=65500{t.Fatalf("price=%+v",q.Price)}
	if q.CreatedAt.Location()!=time.UTC||q.ExpiresAt.Sub(q.CreatedAt)!=5*time.Minute{t.Fatalf("created=%v expires=%v",q.CreatedAt,q.ExpiresAt)}
	want:=time.Date(2026,time.August,30,11,0,0,0,time.UTC)
	if !q.CancellationPolicy.FreeCancelUntil.Equal(want)||q.CancellationPolicy.PolicyCode!="FLEXIBLE"||q.CancellationPolicy.RefundBasisPoints!=10000{t.Fatalf("policy=%+v",q.CancellationPolicy)}
}

func TestCancellationDeadlineUsesHotelLocalCalendarAcrossDST(t *testing.T){
	checkIn:=mustDate(t,2026,time.November,3)
	policy,err:=domain.ResolveCancellationPolicy(checkIn,domain.CancellationPolicyRule{PolicyCode:"FLEX",PolicyVersion:"v1",HotelTimeZone:"America/New_York",FreeCancelDaysBeforeCheckIn:2,FreeCancelLocalHour:18,RefundBasisPoints:10000})
	if err!=nil{t.Fatal(err)}
	want:=time.Date(2026,time.November,1,23,0,0,0,time.UTC) // Nov 1 is EST after the DST fallback.
	if !policy.FreeCancelUntil.Equal(want){t.Fatalf("deadline=%v want=%v",policy.FreeCancelUntil,want)}
}

func TestGetQuoteReturnsImmutableStoredSnapshot(t *testing.T){store:=&quoteMemory{};rates:=&rateMemory{plan:basePlan()};clock:=&fixedClock{now:time.Unix(1000,0)};create,_:=NewCreateQuoteUsecase(store,rates,&sequenceIDs{},clock,time.Hour);created,err:=create.Execute(context.Background(),quoteInput(t));if err!=nil{t.Fatal(err)};rates.plan.NightlyMinor=20000;rates.plan.CancellationRule.RefundBasisPoints=0;got,err:=NewGetQuoteUsecase(store,clock).Execute(context.Background(),created.ID);if err!=nil{t.Fatal(err)};if !reflect.DeepEqual(got,created){t.Fatalf("stored quote mutated: got=%+v want=%+v",got,created)}}
func TestChangedPricingCreatesNewQuote(t *testing.T){store:=&quoteMemory{};rates:=&rateMemory{plan:basePlan()};ids:=&sequenceIDs{};clock:=&fixedClock{now:time.Unix(1000,0)};create,_:=NewCreateQuoteUsecase(store,rates,ids,clock,time.Hour);first,err:=create.Execute(context.Background(),quoteInput(t));if err!=nil{t.Fatal(err)};rates.plan.PricingVersion="v2";rates.plan.NightlyMinor=12000;rates.plan.CancellationRule.PolicyVersion="policy-v2";rates.plan.CancellationRule.RefundBasisPoints=5000;second,err:=create.Execute(context.Background(),quoteInput(t));if err!=nil{t.Fatal(err)};if first.ID==second.ID||first.PricingVersion==second.PricingVersion||first.Price.TotalMinor==second.Price.TotalMinor||first.CancellationPolicy.PolicyVersion==second.CancellationPolicy.PolicyVersion{t.Fatalf("first=%+v second=%+v",first,second)};if store.items[first.ID].CancellationPolicy.RefundBasisPoints!=10000{t.Fatal("first accepted policy was changed")}}
func TestExpiredQuoteIsStructuredOutcome(t *testing.T){store:=&quoteMemory{};rates:=&rateMemory{plan:basePlan()};clock:=&fixedClock{now:time.Unix(1000,0)};create,_:=NewCreateQuoteUsecase(store,rates,&sequenceIDs{},clock,time.Minute);created,err:=create.Execute(context.Background(),quoteInput(t));if err!=nil{t.Fatal(err)};clock.now=created.ExpiresAt;_,err=NewGetQuoteUsecase(store,clock).Execute(context.Background(),created.ID);if !errors.Is(err,domain.ErrQuoteExpired){t.Fatalf("err=%v",err)}}
func TestInvalidDateRangeIsRejectedBeforePersistence(t *testing.T){store:=&quoteMemory{};rates:=&rateMemory{plan:basePlan()};create,_:=NewCreateQuoteUsecase(store,rates,&sequenceIDs{},&fixedClock{now:time.Unix(1000,0)},time.Minute);in:=quoteInput(t);in.CheckOut=in.CheckIn;_,err:=create.Execute(context.Background(),in);if !errors.Is(err,domain.ErrInvalidStay){t.Fatalf("err=%v",err)};if len(store.items)!=0{t.Fatal("invalid quote was persisted")}}
