package billing

import (
	"errors"
	"fmt"
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
	Status BillStatus `json:"status"`
	Items  []LineItem `json:"items"`
	Total  int64      `json:"total"`
}

var (
	ErrBillNotOpen    = errors.New("bill is not open")
	ErrCannotCancel   = errors.New("cannot cancel bill in current state")
	ErrNoPendingItems = errors.New("no pending items to charge")
	ErrDuplicateItem  = func(id string) error { return fmt.Errorf("item %s already exists", id) }
)

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

func (b *Bill) Expire() {
	b.Status = BillExpired
	for i := range b.Items {
		if b.Items[i].Status == ItemPending {
			b.Items[i].Status = ItemCanceled
		}
	}
}

func (b *Bill) PendingCount() int {
	cnt := 0
	for _, it := range b.Items {
		if it.Status == ItemPending {
			cnt++
		}
	}
	return cnt
}
