package billing

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"pave-fees-api/internal/currency"
	"pave-fees-api/internal/data"

	"encore.dev/beta/errs"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

var taskQueue = "billing"

// service setup and lifecycle (initService/Shutdown) follow Encore service struct guidelines:
// https://encore.dev/docs/go/primitives/service-structs

//encore:service
type Service struct {
	temporalClient client.Client
	temporalWorker worker.Worker
}

func initService() (*Service, error) {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return nil, fmt.Errorf("error creating temporal client: %w", err)
	}

	w := worker.New(c, taskQueue, worker.Options{})

	w.RegisterWorkflow(BillWorkflow)
	w.RegisterActivity(ChargeLineItemActivity)
	w.RegisterActivity(RefundLineItemActivity)

	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("error starting termporal worker: %w", err)
	}
	return &Service{temporalClient: c, temporalWorker: w}, nil
}

func (s *Service) Shutdown(ctx context.Context) {
	s.temporalWorker.Stop()
	s.temporalClient.Close()
}

type CreateBillRequest struct {
	AccountID string `json:"account_id"`
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

	if strings.TrimSpace(req.AccountID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "'account_id' is required and must be non-empty"}
	}

	accCur, err := data.LookupAccount(req.AccountID)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: err.Error()}
	}

	if reqCur != accCur {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("currency mismatch: account %s is %s but request was %s", req.AccountID, accCur, reqCur)}
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
		req.AccountID,
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
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to signal billing workflow: " + err.Error()}
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
