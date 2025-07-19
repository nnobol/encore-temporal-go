package billing

import (
	"errors"
	"testing"
	"time"

	"pave-fees-api/internal/currency"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

type UnitTestSuite struct {
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *UnitTestSuite) SetupTest(t *testing.T) {
	s.env = s.NewTestWorkflowEnvironment()
	s.env.RegisterActivity(ChargeLineItemActivity)
	s.env.RegisterActivity(RefundLineItemActivity)
}

func TestUnitTestSuite(t *testing.T) {
	tests := []struct {
		name string
		fn   func(s *UnitTestSuite, t *testing.T)
	}{
		{"BillWorkflow_Settled", (*UnitTestSuite).Test_BillWorkflow_Settled},
		{"BillWorkflow_DuplicateItem", (*UnitTestSuite).Test_BillWorkflow_DuplicateItem},
		{"BillWorkflow_ChargeFail", (*UnitTestSuite).Test_BillWorkflow_ChargeFail},
		{"BillWorkflow_Canceled", (*UnitTestSuite).Test_BillWorkflow_Canceled},
		{"BillWorkflow_Expired", (*UnitTestSuite).Test_BillWorkflow_Expired},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &UnitTestSuite{}
			s.SetupTest(t)
			tc.fn(s, t)
		})
	}
}

func (s *UnitTestSuite) Test_BillWorkflow_Settled(t *testing.T) {
	// add 2 items, then charge
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "a1", Name: "Book", Amount: 1500})
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "b2", Name: "Pen", Amount: 500})
		s.env.SignalWorkflow(SignalChargeBill, nil)
	}, 0)

	s.env.ExecuteWorkflow(
		BillWorkflow,
		"bill-happy",
		"acc-usd",
		currency.USD,
		time.Now().Add(24*time.Hour),
	)

	// make sure workflow finished without issues
	if !s.env.IsWorkflowCompleted() {
		t.Fatal("workflow still running")
	}
	if err := s.env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	// query the final summary
	qr, err := s.env.QueryWorkflow(QueryBill)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	var sum Bill
	if err := qr.Get(&sum); err != nil {
		t.Fatalf("decode query result: %v", err)
	}

	// assert bill and items state
	if sum.Status != BillSettled {
		t.Fatalf("expected SETTLED, got %s", sum.Status)
	}
	if sum.Total != 2000 {
		t.Fatalf("expected total 2000, got %d", sum.Total)
	}
	if len(sum.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(sum.Items))
	}
	for _, it := range sum.Items {
		if it.Status != ItemCharged {
			t.Fatalf("item %s not charged", it.ID)
		}
	}
}

func (s *UnitTestSuite) Test_BillWorkflow_DuplicateItem(t *testing.T) {
	item := LineItem{ID: "dup", Name: "Book", Amount: 123}
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalAddLineItem, item)
		s.env.SignalWorkflow(SignalAddLineItem, item)
		s.env.SignalWorkflow(SignalChargeBill, nil)
	}, 0)

	s.env.ExecuteWorkflow(BillWorkflow, "dup-bill", "acc", currency.USD, time.Now().Add(24*time.Hour))
	if err := s.env.GetWorkflowError(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qr, _ := s.env.QueryWorkflow(QueryBill)
	var sum Bill
	qr.Get(&sum)

	if sum.Status != BillSettled {
		t.Fatalf("want SETTLED, got %s", sum.Status)
	}
	if len(sum.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(sum.Items))
	}
	if sum.Total != 123 {
		t.Fatalf("want total 123, got %d", sum.Total)
	}
}

func (s *UnitTestSuite) Test_BillWorkflow_ChargeFail(t *testing.T) {
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "ok", Name: "Book", Amount: 100})
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "bad", Name: "FAIL", Amount: 50})
		s.env.SignalWorkflow(SignalChargeBill, nil)
	}, 0)

	s.env.ExecuteWorkflow(BillWorkflow, "fail-bill", "acc", currency.USD, time.Now().Add(24*time.Hour))
	err := s.env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected error on partial failure compensation")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != "ChargeCompensated" {
		t.Fatalf("expected ApplicationError ChargeCompensated, got %v", err)
	}
	var failedIDs []string
	appErr.Details(&failedIDs)
	if len(failedIDs) != 1 || failedIDs[0] != "bad" {
		t.Errorf("expected failedIDs=[\"bad\"], got %v", failedIDs)
	}

	qr, _ := s.env.QueryWorkflow(QueryBill)
	var sum Bill
	qr.Get(&sum)
	if sum.Status != BillCompensated {
		t.Errorf("want COMPENSATED, got %s", sum.Status)
	}
	for _, it := range sum.Items {
		var want LineItemStatus
		if it.ID == "bad" {
			want = ItemFailed
		} else {
			want = ItemRefunded
		}
		if it.Status != want {
			t.Errorf("item %s status = %s; want %s", it.ID, it.Status, want)
		}
	}
}

func (s *UnitTestSuite) Test_BillWorkflow_Canceled(t *testing.T) {
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "x1", Name: "Book", Amount: 1500})
		s.env.SignalWorkflow(SignalCancelBill, nil)
	}, 0)

	s.env.ExecuteWorkflow(
		BillWorkflow,
		"bill-cancel",
		"acc-usd",
		currency.USD,
		time.Now().Add(24*time.Hour),
	)

	if !s.env.IsWorkflowCompleted() {
		t.Fatal("workflow still running")
	}
	if err := s.env.GetWorkflowError(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qr, err := s.env.QueryWorkflow(QueryBill)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	var sum Bill
	if err := qr.Get(&sum); err != nil {
		t.Fatalf("decode query result: %v", err)
	}

	if sum.Status != BillCanceled {
		t.Fatalf("expected CANCELED, got %s", sum.Status)
	}
	if len(sum.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(sum.Items))
	}
	if sum.Items[0].Status != ItemCanceled {
		t.Fatalf("expected item CANCELED, got %s", sum.Items[0].Status)
	}
}

func (s *UnitTestSuite) Test_BillWorkflow_Expired(t *testing.T) {
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "a1", Name: "Book", Amount: 1000})
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "b2", Name: "Pen", Amount: 500})
	}, 0)

	s.env.ExecuteWorkflow(
		BillWorkflow,
		"bill-expire",
		"acc-usd",
		currency.USD,
		time.Now().Add(24*time.Hour),
	)

	if !s.env.IsWorkflowCompleted() {
		t.Fatal("workflow still running")
	}
	if err := s.env.GetWorkflowError(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qr, err := s.env.QueryWorkflow(QueryBill)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	var sum Bill
	if err := qr.Get(&sum); err != nil {
		t.Fatalf("decode query result: %v", err)
	}

	if sum.Status != BillExpired {
		t.Fatalf("expected EXPIRED, got %s", sum.Status)
	}
	if len(sum.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(sum.Items))
	}
	for _, it := range sum.Items {
		if it.Status != ItemCanceled {
			t.Fatalf("expected all items CANCELED, got item %s status %s", it.ID, it.Status)
		}
	}
}

func (s *UnitTestSuite) Test_BillWorkflow_ChargeWithNoItems_Expires(t *testing.T) {
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalChargeBill, nil)
	}, 0)
	s.env.ExecuteWorkflow(
		BillWorkflow,
		"no-items-bill",
		"acc-usd",
		currency.USD,
		time.Now().Add(24*time.Hour),
	)
	if !s.env.IsWorkflowCompleted() {
		t.Fatal("workflow still running")
	}
	if err := s.env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	qr, _ := s.env.QueryWorkflow(QueryBill)
	var sum Bill
	qr.Get(&sum)
	if sum.Status != BillExpired {
		t.Errorf("got %s; want EXPIRED", sum.Status)
	}
	if len(sum.Items) != 0 {
		t.Errorf("len(items) = %d; want 0", len(sum.Items))
	}
}

func (s *UnitTestSuite) Test_BillWorkflow_AllItemsFail(t *testing.T) {
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "a1", Name: "FAIL", Amount: 100})
		s.env.SignalWorkflow(SignalAddLineItem, LineItem{ID: "b2", Name: "FAIL", Amount: 200})
		s.env.SignalWorkflow(SignalChargeBill, nil)
	}, 0)

	s.env.ExecuteWorkflow(
		BillWorkflow,
		"fail-all-bill",
		"acc-usd",
		currency.USD,
		time.Now().Add(24*time.Hour),
	)

	err := s.env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected error on all‑items failure")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != "ChargeFailed" {
		t.Fatalf("expected ApplicationError ChargeFailed, got %v", err)
	}
	var failedIDs []string
	appErr.Details(&failedIDs)
	if len(failedIDs) != 2 {
		t.Errorf("expected two failed IDs, got %v", failedIDs)
	}

	qr, _ := s.env.QueryWorkflow(QueryBill)
	var sum Bill
	qr.Get(&sum)
	if sum.Status != BillFailed {
		t.Errorf("want FAILED, got %s", sum.Status)
	}
	if len(sum.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(sum.Items))
	}
	for _, it := range sum.Items {
		if it.Status != ItemFailed {
			t.Errorf("item %s status = %s; want %s", it.ID, it.Status, ItemFailed)
		}
	}
}
