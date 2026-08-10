package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/liemdang260/hotel-booking/services/booking/internal/domain"
)

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Store struct { db DBTX }
func NewStore(db DBTX) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, b *domain.Booking) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO bookings
		(id,user_id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,status,
		 reservation_id,payment_id,version,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14)`,
		b.ID,b.UserID,b.HotelID,b.RoomTypeID,b.CheckIn,b.CheckOut,b.GuestCount,b.RoomQuantity,b.Status,
		b.ReservationID,b.PaymentID,b.Version,b.CreatedAt,b.UpdatedAt)
	return err
}
func (s *Store) Find(ctx context.Context, id string) (*domain.Booking,error) { return s.find(ctx,id,false) }
func (s *Store) Lock(ctx context.Context, id string) (*domain.Booking,error) { return s.find(ctx,id,true) }
func (s *Store) find(ctx context.Context,id string,lock bool)(*domain.Booking,error){
	q:=`SELECT id,user_id,hotel_id,room_type_id,check_in,check_out,guest_count,room_quantity,status,
		COALESCE(reservation_id,''),COALESCE(payment_id,''),version,created_at,updated_at FROM bookings WHERE id=$1`
	if lock { q += " FOR UPDATE" }
	var b domain.Booking
	if err:=s.db.QueryRowContext(ctx,q,id).Scan(&b.ID,&b.UserID,&b.HotelID,&b.RoomTypeID,&b.CheckIn,&b.CheckOut,
		&b.GuestCount,&b.RoomQuantity,&b.Status,&b.ReservationID,&b.PaymentID,&b.Version,&b.CreatedAt,&b.UpdatedAt);err!=nil{
		if errors.Is(err,sql.ErrNoRows){return nil,domain.ErrNotFound};return nil,err
	}
	return &b,nil
}
func (s *Store) Save(ctx context.Context,b *domain.Booking)error{
	res,err:=s.db.ExecContext(ctx,`UPDATE bookings SET status=$1,reservation_id=NULLIF($2,''),
		payment_id=NULLIF($3,''),version=version+1,updated_at=$4 WHERE id=$5 AND version=$6`,
		b.Status,b.ReservationID,b.PaymentID,b.UpdatedAt,b.ID,b.Version)
	if err!=nil{return err}; n,err:=res.RowsAffected();if err!=nil{return err};if n!=1{return domain.ErrConcurrentWrite};b.Version++;return nil
}
func (s *Store) CreatePriceSnapshot(ctx context.Context,p *domain.PriceSnapshot)error{
	_,err:=s.db.ExecContext(ctx,`INSERT INTO booking_price_snapshots
		(booking_id,quote_id,currency,subtotal_minor,tax_minor,service_fee_minor,discount_minor,total_minor,
		 pricing_version,quoted_at,quote_expires_at,accepted_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.BookingID,p.QuoteID,p.Currency,p.SubtotalMinor,p.TaxMinor,p.ServiceFeeMinor,p.DiscountMinor,p.TotalMinor,
		p.PricingVersion,p.QuotedAt,p.QuoteExpiresAt,p.AcceptedAt)
	return err
}
func (s *Store) CreateSaga(ctx context.Context,g *domain.BookingSaga)error{
	_,err:=s.db.ExecContext(ctx,`INSERT INTO booking_sagas
		(id,booking_id,state,reservation_id,payment_id,last_error_code,last_error_message,retry_count,next_retry_at,version,created_at,updated_at)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11,$12)`,
		g.ID,g.BookingID,g.State,g.ReservationID,g.PaymentID,g.LastErrorCode,g.LastErrorMessage,g.RetryCount,g.NextRetryAt,g.Version,g.CreatedAt,g.UpdatedAt)
	return err
}
func (s *Store) SaveSaga(ctx context.Context,g *domain.BookingSaga)error{
	res,err:=s.db.ExecContext(ctx,`UPDATE booking_sagas SET state=$1,reservation_id=NULLIF($2,''),
		payment_id=NULLIF($3,''),last_error_code=NULLIF($4,''),last_error_message=NULLIF($5,''),
		retry_count=$6,next_retry_at=$7,version=version+1,updated_at=$8 WHERE id=$9 AND version=$10`,
		g.State,g.ReservationID,g.PaymentID,g.LastErrorCode,g.LastErrorMessage,g.RetryCount,g.NextRetryAt,g.UpdatedAt,g.ID,g.Version)
	if err!=nil{return err};n,err:=res.RowsAffected();if err!=nil{return err};if n!=1{return domain.ErrConcurrentWrite};g.Version++;return nil
}
func (s *Store) ClaimIdempotency(ctx context.Context,i *domain.IdempotencyRecord)error{
	res,err:=s.db.ExecContext(ctx,`INSERT INTO booking_idempotency
		(id,idempotency_key,request_hash,booking_id,status,response_payload,created_at,updated_at,expires_at)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9) ON CONFLICT(idempotency_key) DO NOTHING`,
		i.ID,i.Key,i.RequestHash,i.BookingID,i.Status,nullableJSON(i.ResponsePayload),i.CreatedAt,i.UpdatedAt,i.ExpiresAt)
	if err!=nil{return err};n,err:=res.RowsAffected();if err!=nil{return err};if n!=1{return domain.ErrIdempotencyConflict};return nil
}
func (s *Store) AddOutbox(ctx context.Context,e *domain.OutboxEvent)error{
	if !json.Valid(e.Payload){return fmt.Errorf("invalid outbox payload")}
	_,err:=s.db.ExecContext(ctx,`INSERT INTO booking_outbox_events
		(id,aggregate_type,aggregate_id,event_type,event_version,payload,status,retry_count,next_retry_at,created_at,published_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ID,e.AggregateType,e.AggregateID,e.EventType,e.EventVersion,e.Payload,e.Status,e.RetryCount,e.NextRetryAt,e.CreatedAt,e.PublishedAt)
	return err
}
func nullableJSON(b []byte) any { if len(b)==0{return nil}; return b }

type Transactor struct { DB *sql.DB }
func (t Transactor) WithinTransaction(ctx context.Context, fn func(context.Context,domain.Repositories)error) error {
	tx,err:=t.DB.BeginTx(ctx,nil);if err!=nil{return err}
	store:=NewStore(tx)
	repos:=domain.Repositories{
		Bookings: bookingRepo{store}, PriceSnapshots: priceRepo{store}, Sagas: sagaRepo{store},
		Idempotency: idempotencyRepo{store}, Outbox: outboxRepo{store},
	}
	if err=fn(ctx,repos);err!=nil{_ = tx.Rollback();return err}
	return tx.Commit()
}
type bookingRepo struct{*Store}
type priceRepo struct{*Store}
func(r priceRepo)Create(c context.Context,p *domain.PriceSnapshot)error{return r.CreatePriceSnapshot(c,p)}
func(r priceRepo)FindByBookingID(context.Context,string)(*domain.PriceSnapshot,error){return nil,domain.ErrNotFound}
type sagaRepo struct{*Store}
func(r sagaRepo)Create(c context.Context,g *domain.BookingSaga)error{return r.CreateSaga(c,g)}
func(r sagaRepo)LockByBookingID(context.Context,string)(*domain.BookingSaga,error){return nil,domain.ErrNotFound}
func(r sagaRepo)Save(c context.Context,g *domain.BookingSaga)error{return r.SaveSaga(c,g)}
type idempotencyRepo struct{*Store}
func(r idempotencyRepo)Claim(c context.Context,i *domain.IdempotencyRecord)error{return r.ClaimIdempotency(c,i)}
func(r idempotencyRepo)FindByKey(context.Context,string)(*domain.IdempotencyRecord,error){return nil,domain.ErrNotFound}
func(r idempotencyRepo)Save(context.Context,*domain.IdempotencyRecord)error{return nil}
type outboxRepo struct{*Store}
func(r outboxRepo)Add(c context.Context,e *domain.OutboxEvent)error{return r.AddOutbox(c,e)}
