package billing

import (
	"errors"
	"fmt"
	"testing"
)

func TestAddItem(t *testing.T) {
	cases := []struct {
		name        string
		startStatus BillStatus
		startItems  []LineItem
		startTotal  int64
		add         LineItem
		wantErrMsg  string
		wantItems   []LineItem
		wantTotal   int64
	}{
		{
			name:        "success",
			startStatus: BillOpen,
			startItems:  nil, startTotal: 0,
			add:        LineItem{ID: "x", Name: "Test", Amount: 100},
			wantErrMsg: "",
			wantItems:  []LineItem{{ID: "x", Name: "Test", Amount: 100, Status: ItemPending}},
			wantTotal:  100,
		},
		{
			name:        "duplicate",
			startStatus: BillOpen,
			startItems:  []LineItem{{ID: "x", Name: "T", Amount: 50, Status: ItemPending}},
			startTotal:  50,
			add:         LineItem{ID: "x", Name: "T", Amount: 50},
			// we expect the message from ErrDuplicateItem("x")
			wantErrMsg: ErrDuplicateItem("x").Error(),
			wantItems:  []LineItem{{ID: "x", Name: "T", Amount: 50, Status: ItemPending}},
			wantTotal:  50,
		},
		{
			name:        "closed",
			startStatus: BillCanceled,
			startItems:  nil,
			startTotal:  0,
			add:         LineItem{ID: "y", Name: "Y", Amount: 10},
			wantErrMsg:  ErrBillNotOpen.Error(),
			wantItems:   nil,
			wantTotal:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bill{
				Status: tc.startStatus,
				Items:  append([]LineItem(nil), tc.startItems...),
				Total:  tc.startTotal,
			}

			err := b.AddItem(tc.add)

			if tc.wantErrMsg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.wantErrMsg)
				}
				if err.Error() != tc.wantErrMsg {
					t.Fatalf("error = %q, want %q", err.Error(), tc.wantErrMsg)
				}
			}

			if len(b.Items) != len(tc.wantItems) {
				t.Fatalf("items len = %d, want %d", len(b.Items), len(tc.wantItems))
			}
			for i := range b.Items {
				if b.Items[i] != tc.wantItems[i] {
					t.Errorf("item[%d] = %+v, want %+v", i, b.Items[i], tc.wantItems[i])
				}
			}

			if b.Total != tc.wantTotal {
				t.Errorf("total = %d, want %d", b.Total, tc.wantTotal)
			}
		})
	}
}

func TestBeginCharge(t *testing.T) {
	cases := []struct {
		name        string
		startStatus BillStatus
		startItems  []LineItem
		wantErr     error
		wantStatus  BillStatus
	}{
		{
			name:        "open with items -> BillCharging",
			startStatus: BillOpen,
			startItems:  []LineItem{{ID: "x", Status: ItemPending}},
			wantErr:     nil,
			wantStatus:  BillCharging,
		},
		{
			name:        "open with no items -> ErrNoPendingItems",
			startStatus: BillOpen,
			startItems:  nil,
			wantErr:     ErrNoPendingItems,
			wantStatus:  BillOpen,
		},
		{
			name:        "charging -> ErrBillNotOpen",
			startStatus: BillCharging,
			startItems:  []LineItem{{ID: "x", Status: ItemPending}},
			wantErr:     ErrBillNotOpen,
			wantStatus:  BillCharging,
		},
		{
			name:        "settled -> ErrBillNotOpen",
			startStatus: BillSettled,
			startItems:  []LineItem{{ID: "x", Status: ItemPending}},
			wantErr:     ErrBillNotOpen,
			wantStatus:  BillSettled,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bill{
				Status: tc.startStatus,
				Items:  append([]LineItem(nil), tc.startItems...),
			}

			err := b.BeginCharge()

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("BeginCharge() error = %v; want %v", err, tc.wantErr)
			}

			if b.Status != tc.wantStatus {
				t.Errorf("Status = %s; want %s", b.Status, tc.wantStatus)
			}
		})
	}
}

func TestCancel(t *testing.T) {
	initial := []LineItem{
		{ID: "p", Status: ItemPending},
		{ID: "c", Status: ItemPending},
	}

	cases := []struct {
		name        string
		startStatus BillStatus
		wantErr     error
		wantStatus  BillStatus
		wantItems   []LineItemStatus
	}{
		{
			name:        "open -> BillCanceled",
			startStatus: BillOpen,
			wantErr:     nil,
			wantStatus:  BillCanceled,
			wantItems:   []LineItemStatus{ItemCanceled, ItemCanceled},
		},
		{
			name:        "charging -> ErrCannotCancel",
			startStatus: BillCharging,
			wantErr:     ErrCannotCancel,
			wantStatus:  BillCharging,
			wantItems:   []LineItemStatus{ItemPending, ItemPending},
		},
		{
			name:        "settled -> ErrCannotCancel",
			startStatus: BillSettled,
			wantErr:     ErrCannotCancel,
			wantStatus:  BillSettled,
			wantItems:   []LineItemStatus{ItemPending, ItemPending},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]LineItem, len(initial))
			copy(items, initial)
			b := &Bill{Status: tc.startStatus, Items: items}

			err := b.Cancel()

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Cancel() error = %v; want %v", err, tc.wantErr)
			}
			if b.Status != tc.wantStatus {
				t.Errorf("Status = %s; want %s", b.Status, tc.wantStatus)
			}
			for i, it := range b.Items {
				if it.Status != tc.wantItems[i] {
					t.Errorf("item[%d].Status = %s; want %s", i, it.Status, tc.wantItems[i])
				}
			}
		})
	}
}

func TestExpire(t *testing.T) {
	initial := []LineItem{
		{ID: "p", Status: ItemPending},
		{ID: "c", Status: ItemPending},
	}

	cases := []struct {
		name        string
		startStatus BillStatus
		wantStatus  BillStatus
		wantItems   []LineItemStatus
	}{
		{
			name:        "open -> expire",
			startStatus: BillOpen,
			wantStatus:  BillExpired,
			wantItems:   []LineItemStatus{ItemCanceled, ItemCanceled},
		},
		{
			name:        "charging -> expire",
			startStatus: BillCharging,
			wantStatus:  BillExpired,
			wantItems:   []LineItemStatus{ItemCanceled, ItemCanceled},
		},
		{
			name:        "settled -> expire",
			startStatus: BillSettled,
			wantStatus:  BillExpired,
			wantItems:   []LineItemStatus{ItemCanceled, ItemCanceled},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]LineItem, len(initial))
			copy(items, initial)
			b := &Bill{Status: tc.startStatus, Items: items}

			b.Expire()

			if b.Status != tc.wantStatus {
				t.Errorf("Expire() status = %s; want %s", b.Status, tc.wantStatus)
			}
			for i, it := range b.Items {
				if it.Status != tc.wantItems[i] {
					t.Errorf("item[%d].Status = %s; want %s", i, it.Status, tc.wantItems[i])
				}
			}
		})
	}
}

func TestPendingCount(t *testing.T) {
	cases := []struct {
		name      string
		statuses  []LineItemStatus
		wantCount int
	}{
		{"none", nil, 0},
		{"all pending", []LineItemStatus{ItemPending, ItemPending}, 2},
		{"mixed", []LineItemStatus{ItemPending, ItemCharged, ItemPending, ItemFailed}, 2},
		{"none pending", []LineItemStatus{ItemCharged, ItemFailed, ItemCanceled}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]LineItem, len(tc.statuses))
			for i, st := range tc.statuses {
				items[i] = LineItem{ID: fmt.Sprint(i), Status: st}
			}
			b := &Bill{Items: items}
			if got := b.PendingCount(); got != tc.wantCount {
				t.Errorf("PendingCount() = %d; want %d", got, tc.wantCount)
			}
		})
	}
}
