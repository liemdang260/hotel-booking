//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

func TestIntegrationBookingCancellationPolicySnapshotIsImmutable(t *testing.T){
	dsn:=os.Getenv("BOOKING_TEST_DATABASE_URL");if dsn==""{t.Skip("set BOOKING_TEST_DATABASE_URL")}
	db,err:=sql.Open("postgres",dsn);if err!=nil{t.Fatal(err)};t.Cleanup(func(){_ = db.Close()})
	ctx:=context.Background()
	bookingID:="00000000-0000-0000-0000-000000000036"
	_,err=db.ExecContext(ctx,`INSERT INTO bookings (id,user_id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,status,version,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,2,1,'PENDING',1,$7,$7)`,bookingID,"00000000-0000-0000-0000-000000000001","hotel-1","deluxe",time.Date(2026,9,1,0,0,0,0,time.UTC),time.Date(2026,9,4,0,0,0,0,time.UTC),time.Date(2026,8,11,5,0,0,0,time.UTC));if err!=nil{t.Fatalf("insert booking: %v",err)}
	store:=NewStore(db);snapshot:=&domain.CancellationPolicySnapshot{BookingID:bookingID,PolicyCode:"FLEXIBLE",PolicyVersion:"policy-v1",FreeCancelUntil:time.Date(2026,8,30,11,0,0,0,time.UTC),RefundBasisPoints:10000,CancellationFeeMinor:0,Currency:"USD",PricingVersion:"v1",CreatedAt:time.Date(2026,8,11,5,0,0,0,time.UTC)}
	if err:=store.CreateCancellationPolicySnapshot(ctx,snapshot);err!=nil{t.Fatalf("create cancellation snapshot: %v",err)}
	got,err:=store.FindCancellationPolicySnapshot(ctx,bookingID);if err!=nil{t.Fatalf("find cancellation snapshot: %v",err)}
	if got.PolicyCode!="FLEXIBLE"||got.PolicyVersion!="policy-v1"||got.RefundBasisPoints!=10000||!got.FreeCancelUntil.Equal(snapshot.FreeCancelUntil){t.Fatalf("got %+v",got)}
	if _,err=db.ExecContext(ctx,`UPDATE booking_cancellation_policies SET refund_basis_points=0 WHERE booking_id=$1`,bookingID);err==nil{t.Fatal("immutable booking cancellation snapshot accepted UPDATE")}
	after,err:=store.FindCancellationPolicySnapshot(ctx,bookingID);if err!=nil{t.Fatal(err)};if after.RefundBasisPoints!=10000{t.Fatalf("snapshot changed after rejected update: %+v",after)}
}
