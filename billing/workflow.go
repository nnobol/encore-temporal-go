package billing

import (
	"fmt"
	"time"

	"pave-fees-api/internal/currency"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// query and signal types/names for the bill workflow
const (
	SignalAddLineItem = "AddLineItem"
	SignalChargeBill  = "ChargeBill"
	SignalCancelBill  = "CancelBill"
	QueryBill         = "QueryBill"
)

func BillWorkflow(ctx workflow.Context, billID string, cur currency.Currency, periodEnd time.Time) error {
	logger := log.With(
		workflow.GetLogger(ctx),
		"bill_id", billID,
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

	bill := &Bill{ID: billID, Status: BillOpen, Currency: cur}

	// set a query handler to handle workflow queries
	err := workflow.SetQueryHandler(ctx, QueryBill, func() (Bill, error) {
		snapshot := append([]LineItem(nil), bill.Items...)
		return Bill{
			ID:       bill.ID,
			Status:   bill.Status,
			Currency: bill.Currency,
			Total:    bill.Total,
			Items:    snapshot,
		}, nil
	})
	if err != nil {
		logger.Error("failed to register query handler", "err", err)
		return err
	}

	// register signal channels to send data to running workflow
	addCh := workflow.GetSignalChannel(ctx, SignalAddLineItem)
	chargeCh := workflow.GetSignalChannel(ctx, SignalChargeBill)
	cancelCh := workflow.GetSignalChannel(ctx, SignalCancelBill)

	// create a timer ctx and set the timer for the workflow
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timer := workflow.NewTimer(timerCtx, periodEnd.Sub(workflow.Now(ctx)))

	selector := workflow.NewSelector(ctx)

	// register callback funcs for the channels and timer for an open bill
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
				if err := bill.Cancel(); err != nil {
					logger.Warn("cancel ignored", "err", err)
					return
				}
				cancelTimer()
				logger.Info("cancel signal received")
			}).
			AddFuture(timer, func(_ workflow.Future) {
				bill.Expire()
				logger.Info("bill expired")
			})

		selector.Select(ctx)
	}

	// switch on bill status
	switch bill.Status {
	case BillCanceled, BillExpired:
		// workflow finished
		return nil
	case BillCharging:
		// 1) charge all pending items asynchronously in their own separate coroutines
		chargeWG := workflow.NewWaitGroup(ctx)
		for i := range bill.Items {
			item := &bill.Items[i]
			if item.Status != ItemPending {
				// charge only pending items
				continue
			}
			chargeWG.Add(1)
			workflow.Go(ctx, func(c workflow.Context) {
				defer chargeWG.Done()
				err := workflow.ExecuteActivity(c, ChargeLineItemActivity, *item).Get(c, nil)

				if err != nil {
					item.Status = ItemFailed
					logger.Warn("item charge failed", "item_id", item.ID, "attempts_exhausted", true, "err", err)
				} else {
					item.Status = ItemCharged
					logger.Info("item charged", "item_id", item.ID, "amount", item.Amount)
				}
			})
		}
		chargeWG.Wait(ctx)

		// 2) count charge failures
		failedCount := 0
		for _, it := range bill.Items {
			if it.Status == ItemFailed {
				failedCount++
			}
		}
		totalItems := len(bill.Items)

		// 3) branch on result
		switch {
		case failedCount == totalItems:
			// all item charges failed -> fail the bill
			if failedCount == totalItems {
				failedIDs := make([]string, 0, failedCount)
				for _, it := range bill.Items {
					failedIDs = append(failedIDs, it.ID)
				}
				bill.Status = BillFailed
				logger.Error("all items failed; bill failed", "failed_items", failedCount)

				return temporal.NewApplicationError(fmt.Sprintf("%d items failed: %v", failedCount, failedIDs), "ChargeFailed", failedIDs)
			}
		case failedCount == 0:
			// none failed -> success -> credit account
			bill.Status = BillSettled
			logger.Info("bill settled")
			// crediting won't fail for demo purposes
			_ = workflow.ExecuteActivity(ctx, CreditAccountActivity, bill.Total, bill.Currency).Get(ctx, nil)
			logger.Info("account credited", "currency", bill.Currency, "amount", bill.Total)
		default:
			// not all item charges failed -> refund the charged items asynchronously
			refundWG := workflow.NewWaitGroup(ctx)
			refundedCount := 0
			for i := range bill.Items {
				item := &bill.Items[i]
				if item.Status == ItemCharged {
					refundWG.Add(1)
					workflow.Go(ctx, func(c workflow.Context) {
						defer refundWG.Done()
						// the refund does not fail for demo purposes
						_ = workflow.ExecuteActivity(c, RefundLineItemActivity, *item).Get(c, nil)
						item.Status = ItemRefunded
						refundedCount++
						logger.Info("item refunded", "item_id", item.ID)
					})
				}
			}
			refundWG.Wait(ctx)

			// mark the bill as compensated due to refunds
			bill.Status = BillCompensated
			logger.Error("bill partially failed and refunded items", "refunded_items", refundedCount, "failed_items", failedCount)
			failedIDs := make([]string, 0, failedCount)
			for _, it := range bill.Items {
				if it.Status == ItemFailed {
					failedIDs = append(failedIDs, it.ID)
				}
			}

			return temporal.NewApplicationError(fmt.Sprintf("refunded %d items after %d failures", refundedCount, failedCount), "ChargeCompensated", failedIDs)
		}

	default:
		logger.Error("unexpected status after selector", "status", bill.Status)
		return temporal.NewNonRetryableApplicationError("invalid state", "", nil)
	}

	return nil
}
