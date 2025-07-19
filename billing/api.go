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
)

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

	var snap BillSummary
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

	return s.temporalClient.SignalWorkflow(ctx, id, "", SignalAddLineItem, li)
}
