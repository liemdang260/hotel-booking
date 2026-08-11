package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixedClock struct{ now time.Time }
func (c fixedClock) Now() time.Time { return c.now }

type fakeRepository struct {
	events []Event
	claim ClaimRequest
	published []string
	retried []string
}
func (r *fakeRepository) Claim(_ context.Context, request ClaimRequest) ([]Event,error) {
	r.claim=request
	events:=append([]Event(nil),r.events...)
	for i:=range events { events[i].ClaimToken=request.ClaimToken }
	return events,nil
}
func (r *fakeRepository) MarkPublished(_ context.Context,id,token string,_ time.Time)error {
	if token!=r.claim.ClaimToken{return errors.New("wrong token")}
	r.published=append(r.published,id);return nil
}
func (r *fakeRepository) MarkRetry(_ context.Context,id,token string,_ time.Time,_ string)error {
	if token!=r.claim.ClaimToken{return errors.New("wrong token")}
	r.retried=append(r.retried,id);return nil
}
type fakePublisher struct{ fail map[string]error }
func(p fakePublisher)Publish(_ context.Context,event Event)error{return p.fail[event.ID]}

func TestPublishBatchBoundsClaimsAndMarksSuccess(t *testing.T){
	repo:=&fakeRepository{events:[]Event{{ID:"one"},{ID:"two"}}}
	u,err:=NewPublishBatch(repo,fakePublisher{},fixedClock{time.Unix(100,0)},Config{MaxBatchSize:2,LeaseDuration:time.Minute,Backoff:DefaultBackoff})
	if err!=nil{t.Fatal(err)}
	result,err:=u.ExecuteBatch(context.Background(),100)
	if err!=nil{t.Fatal(err)}
	if repo.claim.Limit!=2||result.Claimed!=2||result.Published!=2||len(repo.published)!=2{t.Fatalf("unexpected result: %#v claim=%#v",result,repo.claim)}
	if !repo.claim.LeaseUntil.Equal(repo.claim.Now.Add(time.Minute)){t.Fatal("lease not bounded")}
}
func TestPublishBatchLeavesTemporaryFailureRetryable(t *testing.T){
	repo:=&fakeRepository{events:[]Event{{ID:"one"}}}
	u,err:=NewPublishBatch(repo,fakePublisher{fail:map[string]error{"one":errors.New("kafka unavailable")}},fixedClock{time.Unix(100,0)},Config{MaxBatchSize:10,LeaseDuration:time.Minute,Backoff:DefaultBackoff})
	if err!=nil{t.Fatal(err)}
	result,err:=u.ExecuteBatch(context.Background(),1)
	if err==nil{t.Fatal("expected publish failure")}
	if result.Retried!=1||len(repo.retried)!=1||len(repo.published)!=0{t.Fatalf("unexpected result: %#v",result)}
}
func TestNewPublishBatchRejectsInvalidConfiguration(t *testing.T){
	if _,err:=NewPublishBatch(nil,nil,nil,Config{});!errors.Is(err,ErrInvalidConfiguration){t.Fatalf("got %v",err)}
}
