//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/liemdang260/hotel-booking/services/pricing/internal/domain"
)

func openPricingIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PRICING_TEST_DATABASE_URL")
	if dsn == "" { t.Skip("set PRICING_TEST_DATABASE_URL to a disposable migrated PostgreSQL database") }
	db,err:=sql.Open("postgres",dsn);if err!=nil{t.Fatalf("open pricing integration database: %v",err)}
	if err:=db.PingContext(context.Background());err!=nil{_ = db.Close();t.Fatalf("ping pricing integration database: %v",err)}
	t.Cleanup(func(){_ = db.Close()});return db
}

func integrationQuote(t *testing.T) domain.Quote {
	t.Helper();checkIn,err:=domain.NewDate(2026,time.September,1);if err!=nil{t.Fatal(err)};checkOut,err:=domain.NewDate(2026,time.September,4);if err!=nil{t.Fatal(err)}
	now:=time.Date(2026,8,11,2,0,0,0,time.UTC)
	return domain.Quote{ID:"quote-integration-1",Input:domain.QuoteInput{HotelID:"hotel-1",RoomTypeID:"deluxe",CheckIn:checkIn,CheckOut:checkOut,GuestCount:2,RoomQuantity:2},Price:domain.PriceBreakdown{SubtotalMinor:60000,TaxMinor:6000,ServiceFeeMinor:500,DiscountMinor:1000,TotalMinor:65500},Currency:"USD",PricingVersion:"v1",CancellationPolicy:domain.CancellationPolicy{PolicyCode:"FLEXIBLE",PolicyVersion:"policy-v1",FreeCancelUntil:time.Date(2026,8,30,11,0,0,0,time.UTC),RefundBasisPoints:10000,CancellationFeeMinor:0},CreatedAt:now,ExpiresAt:now.Add(5*time.Minute)}
}

func TestIntegrationQuoteRepositoryPersistsImmutableDateSnapshot(t *testing.T){
	db:=openPricingIntegrationDB(t);if _,err:=db.Exec(`TRUNCATE TABLE quotes`);err!=nil{t.Fatalf("truncate quotes: %v",err)}
	quote:=integrationQuote(t);repo:=NewQuoteRepository(db);if err:=repo.Insert(context.Background(),quote);err!=nil{t.Fatalf("insert quote: %v",err)}
	got,err:=repo.Get(context.Background(),quote.ID);if err!=nil{t.Fatalf("get quote: %v",err)}
	if got.ID!=quote.ID||got.Input.CheckIn!=quote.Input.CheckIn||got.Input.CheckOut!=quote.Input.CheckOut||got.Price.TotalMinor!=65500||got.PricingVersion!="v1"||got.CancellationPolicy.PolicyCode!="FLEXIBLE"||got.CancellationPolicy.PolicyVersion!="policy-v1"||!got.CancellationPolicy.FreeCancelUntil.Equal(quote.CancellationPolicy.FreeCancelUntil)||got.CancellationPolicy.RefundBasisPoints!=10000{t.Fatalf("unexpected stored quote: %+v",got)}
	_,err=db.Exec(`UPDATE quotes SET refund_basis_points=0 WHERE id=$1`,quote.ID);if err==nil{t.Fatal("immutable quote accepted cancellation-policy UPDATE")}
	after,err:=repo.Get(context.Background(),quote.ID);if err!=nil{t.Fatalf("get quote after rejected update: %v",err)};if after.CancellationPolicy.RefundBasisPoints!=10000{t.Fatalf("policy changed after rejected update: %+v",after.CancellationPolicy)}
}

func TestIntegrationQuoteSchemaRejectsInvalidStayAndTotals(t *testing.T){
	db:=openPricingIntegrationDB(t);if _,err:=db.Exec(`TRUNCATE TABLE quotes`);err!=nil{t.Fatalf("truncate quotes: %v",err)};now:=time.Date(2026,8,11,2,0,0,0,time.UTC)
	_,err:=db.Exec(`INSERT INTO quotes (id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,subtotal_minor,tax_minor,service_fee_minor,discount_minor,total_minor,currency,pricing_version,cancellation_policy_code,cancellation_policy_version,free_cancel_until,refund_basis_points,cancellation_fee_minor,created_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,"invalid-quote","hotel-1","deluxe","2026-09-04","2026-09-01",2,1,10000,1000,0,0,9999,"USD","v1","FLEXIBLE","policy-v1",now,10000,0,now,now.Add(time.Minute))
	if err==nil{t.Fatal("invalid stay/totals unexpectedly persisted")};if errors.Is(err,sql.ErrNoRows){t.Fatalf("unexpected error type: %v",err)}
}
