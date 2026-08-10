package usecase

import (
	"context"
	"errors"
	"testing"
	"time"
)
type recoveryStore struct{sagas []SagaSnapshot;marked,retried int;next time.Time;claimed bool}
func(s *recoveryStore)ClaimDue(context.Context,time.Time,int,time.Duration)([]SagaSnapshot,error){
	if s.claimed{return nil,nil};s.claimed=true;return s.sagas,nil
}
func(s *recoveryStore)MarkRecovered(context.Context,string,int64)error{s.marked++;return nil}
func(s *recoveryStore)ScheduleRetry(_ context.Context,_ string,_ int64,next time.Time,_ string)error{s.retried++;s.next=next;return nil}
type resumer struct{calls int;err error;bookingIDs []string}
func(r *resumer)Resume(_ context.Context,s SagaSnapshot)error{r.calls++;r.bookingIDs=append(r.bookingIDs,s.BookingID);return r.err}
func TestRestartDoesNotReclaimAlreadyLeasedSaga(t *testing.T){
	store:=&recoveryStore{sagas:[]SagaSnapshot{{ID:"s1",BookingID:"b1",Version:7}}};resume:=&resumer{}
	u:=NewRecoveryUsecase(store,resume)
	n,err:=u.RecoverDue(context.Background(),10);if err!=nil||n!=1{t.Fatalf("n=%d err=%v",n,err)}
	n,err=u.RecoverDue(context.Background(),10);if err!=nil||n!=0{t.Fatalf("second n=%d err=%v",n,err)}
	if resume.calls!=1||store.marked!=1{t.Fatalf("calls=%d marked=%d",resume.calls,store.marked)}
}
func TestRetryIsScheduledWithBoundedBackoff(t *testing.T){
	now:=time.Unix(1000,0)
	store:=&recoveryStore{sagas:[]SagaSnapshot{{ID:"s1",BookingID:"b1",Version:2,RetryCount:20}}}
	u:=NewRecoveryUsecase(store,&resumer{err:ErrRetryableRecovery});u.now=func()time.Time{return now}
	n,err:=u.RecoverDue(context.Background(),1);if err!=nil||n!=1{t.Fatalf("n=%d err=%v",n,err)}
	if store.retried!=1||store.next.Sub(now)!=time.Minute{t.Fatalf("retry=%d next=%v",store.retried,store.next)}
}
func TestConcurrentVersionLossIsBenign(t *testing.T){
	store:=&recoveryStore{sagas:[]SagaSnapshot{{ID:"s1",BookingID:"b1",Version:3}}}
	u:=NewRecoveryUsecase(store,&resumer{err:ErrConcurrentRecovery})
	n,err:=u.RecoverDue(context.Background(),1)
	if err!=nil||n!=0||!errors.Is(ErrConcurrentRecovery,ErrConcurrentRecovery){t.Fatalf("n=%d err=%v",n,err)}
}
