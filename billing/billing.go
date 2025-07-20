package billing

import (
	"errors"
	"fmt"
	"pave-fees-api/internal/currency"
)

type LineItemStatus string
type BillStatus string

const (
	ItemPending  LineItemStatus = "PENDING"
	ItemCharged  LineItemStatus = "CHARGED"
	ItemFailed   LineItemStatus = "FAILED"
	ItemCanceled LineItemStatus = "CANCELED"
	ItemRefunded LineItemStatus = "REFUNDED"
)

const (
	BillOpen        BillStatus = "OPEN"
	BillCharging    BillStatus = "CHARGING"
	BillSettled     BillStatus = "SETTLED"
	BillCanceled    BillStatus = "CANCELED"
	BillExpired     BillStatus = "EXPIRED"
	BillFailed      BillStatus = "FAILED"
	BillCompensated BillStatus = "COMPENSATED"
)

type LineItem struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Amount int64          `json:"amount"`
	Status LineItemStatus `json:"status"`
}

type Bill struct {
	ID       string            `json:"id"`
	Status   BillStatus        `json:"status"`
	Currency currency.Currency `json:"currency"`
	Items    []LineItem        `json:"items"`
	Total    int64             `json:"total"`
}

var (
	ErrBillNotOpen    = errors.New("bill is not open")
	ErrCannotCancel   = errors.New("cannot cancel bill in current state")
	ErrNoPendingItems = errors.New("no pending items to charge")
	ErrDuplicateItem  = func(id string) error { return fmt.Errorf("item %s already exists", id) }
)

// adds item to bill only when the bill is open and the same item is not already added
func (b *Bill) AddItem(li LineItem) error {
	if b.Status != BillOpen {
		return ErrBillNotOpen
	}
	for _, it := range b.Items {
		if it.ID == li.ID {
			return ErrDuplicateItem(li.ID)
		}
	}
	li.Status = ItemPending
	b.Items = append(b.Items, li)
	b.Total += li.Amount
	return nil
}

// begin charging items in the bill, set the appropriate state to indicate that
// and charge only when we have pending items in the bill
func (b *Bill) BeginCharge() error {
	if b.Status != BillOpen {
		return ErrBillNotOpen
	}
	if b.PendingCount() == 0 {
		return ErrNoPendingItems
	}
	b.Status = BillCharging
	return nil
}

// cancel/close an open bill and its pending items
func (b *Bill) Cancel() error {
	if b.Status != BillOpen {
		return ErrCannotCancel
	}
	b.Status = BillCanceled
	for i := range b.Items {
		if b.Items[i].Status == ItemPending {
			b.Items[i].Status = ItemCanceled
		}
	}
	return nil
}

// expire a bill and its items
// no need to check bill status because the way our workflow is set up, expire will fire only on an open bill
func (b *Bill) Expire() {
	b.Status = BillExpired
	for i := range b.Items {
		if b.Items[i].Status == ItemPending {
			b.Items[i].Status = ItemCanceled
		}
	}
}

// get the pending item count of a bill
func (b *Bill) PendingCount() int {
	cnt := 0
	for _, it := range b.Items {
		if it.Status == ItemPending {
			cnt++
		}
	}
	return cnt
}
