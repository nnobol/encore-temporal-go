package billing

import (
	"strconv"
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
		wantErrMsg  string
		wantStatus  BillStatus
	}{
		{"from_open", BillOpen, "", BillCharging},
		{"from_charging", BillCharging, ErrBillNotOpen.Error(), BillCharging},
		{"from_settled", BillSettled, ErrBillNotOpen.Error(), BillSettled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bill{Status: tc.startStatus}
			err := b.BeginCharge()

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

			if b.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", b.Status, tc.wantStatus)
			}
		})
	}
}

func TestValidateCharges(t *testing.T) {
	cases := []struct {
		name          string
		startStatuses []LineItemStatus
		wantStatus    BillStatus
		wantFails     int
	}{
		{name: "all succeed", startStatuses: []LineItemStatus{ItemCharged, ItemCharged}, wantStatus: BillSettled, wantFails: 0},
		{name: "one fails", startStatuses: []LineItemStatus{ItemFailed, ItemCharged}, wantStatus: BillFailed, wantFails: 1},
		{name: "all fail", startStatuses: []LineItemStatus{ItemFailed, ItemFailed}, wantStatus: BillFailed, wantFails: 2},
		{name: "no items", startStatuses: nil, wantStatus: BillSettled, wantFails: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]LineItem, len(tc.startStatuses))
			for i, st := range tc.startStatuses {
				items[i] = LineItem{ID: strconv.Itoa(i), Status: st}
			}
			b := &Bill{Status: BillCharging, Items: items}
			gotFails := b.ValidateCharges()
			if gotFails != tc.wantFails {
				t.Errorf("ValidateCharges fail count = %d; want %d", gotFails, tc.wantFails)
			}
			if b.Status != tc.wantStatus {
				t.Errorf("ValidateCharges status = %s; want %s", b.Status, tc.wantStatus)
			}
		})
	}
}

func TestCancelAndExpire(t *testing.T) {
	initial := []LineItem{{ID: "p", Status: ItemPending}, {ID: "c", Status: ItemCharged}}
	cases := []struct {
		name       string
		action     func(b *Bill)
		wantStatus BillStatus
		wantItems  []LineItemStatus
	}{
		{name: "cancel", action: (*Bill).Cancel, wantStatus: BillCanceled, wantItems: []LineItemStatus{ItemCanceled, ItemCharged}},
		{name: "expire", action: (*Bill).Expire, wantStatus: BillExpired, wantItems: []LineItemStatus{ItemCanceled, ItemCharged}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]LineItem, len(initial))
			copy(items, initial)
			b := &Bill{Status: BillOpen, Items: items}

			tc.action(b)

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
