// Package billing implements the core billing service, exposing a set of Encore API endpoints
// that allow for bill creation, item management, and bill state changes. It integrates with
// Temporal to manage long-running billing processes asynchronously and reliably.
package billing

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"pave-fees-api/internal/currency"

	"encore.dev/beta/errs"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

var taskQueue = "billing"

// Service encapsulates the Temporal client and worker used by the billing service
// to orchestrate billing workflows and activities.
//
//encore:service
type Service struct {
	temporalClient client.Client
	temporalWorker worker.Worker
}

// initService initializes the Temporal client and worker for the billing service.
// It registers the workflow and activities and starts the worker.
// This function is called automatically by Encore when the service starts.
func initService() (*Service, error) {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return nil, fmt.Errorf("error creating temporal client: %w", err)
	}

	w := worker.New(c, taskQueue, worker.Options{})

	w.RegisterWorkflow(BillWorkflow)
	w.RegisterActivity(ChargeLineItemActivity)
	w.RegisterActivity(RefundLineItemActivity)
	w.RegisterActivity(CreditAccountActivity)

	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("error starting termporal worker: %w", err)
	}
	return &Service{temporalClient: c, temporalWorker: w}, nil
}

// Shutdown gracefully stops the Temporal worker and closes the client connection.
// This is called automatically when the Encore service is shut down.
func (s *Service) Shutdown(ctx context.Context) {
	s.temporalWorker.Stop()
	s.temporalClient.Close()
}

type CreateBillRequest struct {
	Currency  string `json:"currency"`
	PeriodEnd string `json:"period_end,omitempty"`
}

type CreateBillResponse struct {
	BillID string `json:"bill_id"`
}

//encore:api public method=POST path=/bills
func (s *Service) CreateBill(ctx context.Context, req CreateBillRequest) (*CreateBillResponse, error) {
	if strings.TrimSpace(req.Currency) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "'currency' is required and must be non-empty"}
	}

	reqCur, err := currency.Parse(req.Currency)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}

	var periodEnd time.Time
	if strings.TrimSpace(req.PeriodEnd) == "" {
		periodEnd = time.Now().UTC().Add(30 * 24 * time.Hour) // default +30 days
	} else {
		parsed, err := time.Parse(time.RFC3339, req.PeriodEnd)
		if err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "'period_end' must be RFC3339"}
		}
		if !parsed.After(time.Now()) {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "period_end must be a future date"}
		}
		periodEnd = parsed.UTC()
	}

	b := make([]byte, 8)
	rand.Read(b)
	billID := base64.RawURLEncoding.EncodeToString(b)

	_, err = s.temporalClient.ExecuteWorkflow(ctx,
		client.StartWorkflowOptions{
			ID:        billID,
			TaskQueue: taskQueue,
		},
		BillWorkflow,
		billID,
		reqCur,
		periodEnd,
	)

	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to start workflow: " + err.Error()}
	}

	return &CreateBillResponse{BillID: billID}, nil
}

type AddItemRequest struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Amount int64  `json:"amount"`
}

//encore:api public method=POST path=/bills/:id/items
func (s *Service) AddItem(ctx context.Context, id string, req AddItemRequest) error {
	if strings.TrimSpace(req.ID) == "" {
		return &errs.Error{Code: errs.InvalidArgument, Message: "'id' is required and must be non-empty"}
	}

	if req.Amount <= 0 {
		return &errs.Error{Code: errs.InvalidArgument, Message: "'amount' must be greater than 0"}
	}

	if strings.TrimSpace(req.Name) == "" {
		return &errs.Error{Code: errs.InvalidArgument, Message: "'name' is required and must be non-empty"}
	}

	qr, err := s.temporalClient.QueryWorkflow(ctx, id, "", QueryBill)
	if err != nil {
		return &errs.Error{Code: errs.NotFound, Message: "bill not found"}
	}

	var snap Bill
	if err := qr.Get(&snap); err != nil {
		return err
	}

	if snap.Status != BillOpen {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "bill not open"}
	}

	for _, item := range snap.Items {
		if item.ID == req.ID {
			return &errs.Error{Code: errs.AlreadyExists, Message: "item already exists in the bill"}
		}
	}

	li := LineItem{
		ID:     req.ID,
		Name:   req.Name,
		Amount: req.Amount,
		Status: ItemPending,
	}

	if err := s.temporalClient.SignalWorkflow(ctx, id, "", SignalAddLineItem, li); err != nil {
		return &errs.Error{Code: errs.Internal, Message: "failed to signal billing workflow: " + err.Error()}
	}

	return nil
}

//encore:api public method=POST path=/bills/:id/charge
func (s *Service) ChargeBill(ctx context.Context, id string) (*Bill, error) {
	qr, err := s.temporalClient.QueryWorkflow(ctx, id, "", QueryBill)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "bill not found"}
	}
	var summary Bill
	if err := qr.Get(&summary); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	if summary.Status != BillOpen {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: fmt.Sprintf("cannot charge bill in status %s", summary.Status),
		}
	}

	if summary.PendingCount() == 0 {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: "cannot charge bill with no pending items",
		}
	}

	if err := s.temporalClient.SignalWorkflow(ctx, id, "", SignalChargeBill, nil); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to signal workflow for charge: " + err.Error()}
	}

	qr2, err := s.temporalClient.QueryWorkflow(ctx, id, "", QueryBill)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
	if err := qr2.Get(&summary); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	return &summary, nil
}

//encore:api public method=POST path=/bills/:id/cancel
func (s *Service) CancelBill(ctx context.Context, id string) (*Bill, error) {
	qr, err := s.temporalClient.QueryWorkflow(ctx, id, "", QueryBill)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "bill not found"}
	}
	var bill Bill
	if err := qr.Get(&bill); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	if bill.Status != BillOpen {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: fmt.Sprintf("cannot cancel bill in status %s", bill.Status),
		}
	}

	if err := s.temporalClient.SignalWorkflow(ctx, id, "", SignalCancelBill, nil); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to signal workflow for cancel: " + err.Error()}
	}

	qr2, err := s.temporalClient.QueryWorkflow(ctx, id, "", QueryBill)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
	if err := qr2.Get(&bill); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}

	return &bill, nil
}

//encore:api public method=GET path=/bills/:id
func (s *Service) GetBill(ctx context.Context, id string) (*Bill, error) {

	qr, err := s.temporalClient.QueryWorkflow(ctx, id, "", QueryBill)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "bill not found"}
	}
	var bill Bill
	if err := qr.Get(&bill); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
	return &bill, nil
}
