package worker

import "context"

type CancellationRecovery interface{ Execute(context.Context)error }
type CancellationRecoveryWorker struct{ recover CancellationRecovery }
func NewCancellationRecoveryWorker(r CancellationRecovery)*CancellationRecoveryWorker{return &CancellationRecoveryWorker{recover:r}}
func(w *CancellationRecoveryWorker)RunOnce(ctx context.Context)error{return w.recover.Execute(ctx)}
