package worker

import (
	"context"
	"testing"
	"time"
)
type executor struct{calls int}
func(e *executor)RecoverDue(context.Context,int)(int,error){e.calls++;return 1,nil}
func TestRunOnceDelegatesToRecoveryUsecase(t *testing.T){
	e:=&executor{};w:=NewRecoveryWorker(e,time.Minute,25)
	n,err:=w.RunOnce(context.Background())
	if err!=nil||n!=1||e.calls!=1{t.Fatalf("n=%d calls=%d err=%v",n,e.calls,err)}
}
