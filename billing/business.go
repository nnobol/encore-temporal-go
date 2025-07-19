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
)

const (
	BillOpen     BillStatus = "OPEN"
	BillCharging BillStatus = "CHARGING"
	BillSettled  BillStatus = "SETTLED"
	BillCanceled BillStatus = "CANCELED"
	BillExpired  BillStatus = "EXPIRED"
	BillFailed   BillStatus = "FAILED"
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
	ErrBillNotOpen   = errors.New("bill is not open")
	ErrDuplicateItem = func(id string) error { return fmt.Errorf("item %s already exists", id) }
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
	b.Status = BillCharging
	return nil
}

func (b *Bill) ValidateCharges() int {
	failCount := 0
	for _, it := range b.Items {
		if it.Status == ItemFailed {
			failCount++
		}
	}
	if failCount > 0 {
		b.Status = BillFailed
	} else {
		b.Status = BillSettled
	}
	return failCount
}

func (b *Bill) Cancel() {
	b.Status = BillCanceled
	for i := range b.Items {
		if b.Items[i].Status == ItemPending {
			b.Items[i].Status = ItemCanceled
		}
	}
}

func (b *Bill) Expire() {
	b.Status = BillExpired
	for i := range b.Items {
		if b.Items[i].Status == ItemPending {
			b.Items[i].Status = ItemCanceled
		}
	}
}
