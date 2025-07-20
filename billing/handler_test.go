package billing

import (
	"context"
	"testing"
	"time"
)

func TestCreateBill(t *testing.T) {
	svc, err := initService()
	if err != nil {
		t.Fatalf("failed to init service: %v", err)
	}
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	resp, err := svc.CreateBill(ctx, CreateBillRequest{
		Currency:  "USD",
		PeriodEnd: time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	})

	if err != nil {
		t.Fatalf("CreateBill returned error: %v", err)
	}
	if resp.BillID == "" {
		t.Error("expected non-empty bill ID")
	}
}

func TestAddItemToBill(t *testing.T) {
	svc, err := initService()
	if err != nil {
		t.Fatalf("failed to init service: %v", err)
	}
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	billResp, _ := svc.CreateBill(ctx, CreateBillRequest{Currency: "USD"})
	billID := billResp.BillID

	err = svc.AddItem(ctx, billID, AddItemRequest{
		ID:     "item-1",
		Name:   "Test Item",
		Amount: 100,
	})

	if err != nil {
		t.Fatalf("AddItem returned error: %v", err)
	}

	bill, err := svc.GetBill(ctx, billID)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}

	if len(bill.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(bill.Items))
	}
}

func TestChargeBill_Success(t *testing.T) {
	svc, err := initService()
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	resp, _ := svc.CreateBill(ctx, CreateBillRequest{Currency: "USD"})
	id := resp.BillID

	svc.AddItem(ctx, id, AddItemRequest{
		ID:     "item-1",
		Name:   "Subscription",
		Amount: 200,
	})

	result, err := svc.ChargeBill(ctx, id)
	if err != nil {
		t.Fatalf("ChargeBill failed: %v", err)
	}

	if result.Status != BillCharging {
		t.Errorf("expected bill to be charging, got %s", result.Status)
	}
}

func TestCancelBill_Success(t *testing.T) {
	svc, err := initService()
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	resp, _ := svc.CreateBill(ctx, CreateBillRequest{Currency: "USD"})
	id := resp.BillID

	cancelled, err := svc.CancelBill(ctx, id)
	if err != nil {
		t.Fatalf("CancelBill failed: %v", err)
	}
	if cancelled.Status != BillCanceled {
		t.Errorf("expected status to be Canceled, got %s", cancelled.Status)
	}
}

func TestAddItemAfterCharge_Fails(t *testing.T) {
	svc, _ := initService()
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	resp, _ := svc.CreateBill(ctx, CreateBillRequest{Currency: "USD"})
	id := resp.BillID

	svc.AddItem(ctx, id, AddItemRequest{ID: "1", Name: "A", Amount: 100})
	svc.ChargeBill(ctx, id)

	err := svc.AddItem(ctx, id, AddItemRequest{ID: "2", Name: "B", Amount: 50})
	if err == nil {
		t.Fatal("expected error when adding item to a charged bill, got nil")
	}
}

func TestDuplicateItemFails(t *testing.T) {
	svc, _ := initService()
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	resp, _ := svc.CreateBill(ctx, CreateBillRequest{Currency: "USD"})
	id := resp.BillID

	item := AddItemRequest{ID: "item-1", Name: "A", Amount: 100}
	svc.AddItem(ctx, id, item)
	err := svc.AddItem(ctx, id, item)
	if err == nil {
		t.Fatal("expected error on duplicate item ID")
	}
}

func TestCreateBill_InvalidCurrency(t *testing.T) {
	svc, _ := initService()
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	_, err := svc.CreateBill(ctx, CreateBillRequest{
		Currency: "XYZ",
	})
	if err == nil {
		t.Fatal("expected error for unsupported currency")
	}
}

func TestGetBill_AfterMultipleAdds(t *testing.T) {
	svc, _ := initService()
	defer svc.Shutdown(context.Background())

	ctx := context.Background()
	resp, _ := svc.CreateBill(ctx, CreateBillRequest{Currency: "USD"})
	id := resp.BillID

	svc.AddItem(ctx, id, AddItemRequest{ID: "1", Name: "One", Amount: 100})
	svc.AddItem(ctx, id, AddItemRequest{ID: "2", Name: "Two", Amount: 50})

	bill, err := svc.GetBill(ctx, id)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}
	if len(bill.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(bill.Items))
	}
	if bill.Total != 150 {
		t.Errorf("expected total to be 150, got %d", bill.Total)
	}
}
