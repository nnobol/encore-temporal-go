package billing

import (
	"context"
	"fmt"
	"time"

	"pave-fees-api/internal/currency"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	SignalAddLineItem = "AddLineItem"
	SignalChargeBill  = "ChargeBill"
	SignalCancelBill  = "CancelBill"
	QueryBill         = "QueryBill"
)

type BillSummary struct {
	Status BillStatus `json:"status"`
	Total  int64      `json:"total"`
	Items  []LineItem `json:"items"`
}

func BillWorkflow(ctx workflow.Context, billID, acctID string, cur currency.Currency, periodEnd time.Time) error {
	logger := log.With(
		workflow.GetLogger(ctx),
		"bill_id", billID,
		"account_id", acctID,
		"currency", cur,
	)

	logger.Info("workflow started")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 3,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	bill := &Bill{Status: BillOpen}

	err := workflow.SetQueryHandler(ctx, QueryBill, func() (BillSummary, error) {
		snapshot := append([]LineItem(nil), bill.Items...)
		return BillSummary{
			Status: bill.Status,
			Total:  bill.Total,
			Items:  snapshot,
		}, nil
	})
	if err != nil {
		logger.Error("failed to register query handler", "err", err)
		return err
	}

	addCh := workflow.GetSignalChannel(ctx, SignalAddLineItem)
	chargeCh := workflow.GetSignalChannel(ctx, SignalChargeBill)
	cancelCh := workflow.GetSignalChannel(ctx, SignalCancelBill)

	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timer := workflow.NewTimer(timerCtx, periodEnd.Sub(workflow.Now(ctx)))

	selector := workflow.NewSelector(ctx)

	for bill.Status == BillOpen {
		selector.
			AddReceive(addCh, func(c workflow.ReceiveChannel, _ bool) {
				var li LineItem
				c.Receive(ctx, &li)
				if err := bill.AddItem(li); err != nil {
					logger.Warn("add-item ignored", "err", err)
					return
				}
				logger.Info("item added", "item_id", li.ID, "amount", li.Amount, "new_total", bill.Total)
			}).
			AddReceive(chargeCh, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				if err := bill.BeginCharge(); err != nil {
					logger.Warn("charge ignored", "err", err)
					return
				}
				cancelTimer()
				logger.Info("charge signal received")
			}).
			AddReceive(cancelCh, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				bill.Cancel()
				cancelTimer()
				logger.Info("cancel signal received")
			}).
			AddFuture(timer, func(_ workflow.Future) {
				bill.Expire()
				logger.Info("bill expired")
			})

		selector.Select(ctx)
	}

	switch bill.Status {
	case BillCanceled, BillExpired:
		return nil
	case BillCharging:
		wg := workflow.NewWaitGroup(ctx)
		wg.Add(len(bill.Items))
		for i := range bill.Items {
			idx := i
			workflow.Go(ctx, func(c workflow.Context) {
				defer wg.Done()
				it := bill.Items[idx]
				err := workflow.ExecuteActivity(c, ChargeLineItemActivity, it).Get(c, nil)

				if err != nil {
					bill.Items[idx].Status = ItemFailed
					logger.Warn("item charge failed", "item_id", it.ID, "attempts_exhausted", true, "err", err)
				} else {
					bill.Items[idx].Status = ItemCharged
					logger.Info("item charged", "item_id", it.ID, "amount", it.Amount)
				}
			})
		}
		wg.Wait(ctx)

		failedCount := bill.ValidateCharges()

		if failedCount > 0 {
			failedIDs := make([]string, 0)
			for _, it := range bill.Items {
				if it.Status == ItemFailed {
					failedIDs = append(failedIDs, it.ID)
				}
			}

			logger.Error("bill failed", "failed_items", failedCount)
			return temporal.NewApplicationError(fmt.Sprintf("%d items failed: %v", failedCount, failedIDs), "ChargeFailed", failedIDs)
		} else {
			logger.Info("bill settled")
		}
	default:
		logger.Error("unexpected status after selector", "status", bill.Status)
		return temporal.NewNonRetryableApplicationError("invalid state", "", nil)
	}

	return nil
}

func ChargeLineItemActivity(_ context.Context, li LineItem) error {
	time.Sleep(100 * time.Millisecond)
	if li.Name == "FAIL" {
		return fmt.Errorf("simulated failure for %s", li.ID)
	}
	return nil
}
