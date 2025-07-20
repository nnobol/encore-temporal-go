package account

import (
	"context"
	"errors"
	"testing"

	"pave-fees-api/internal/currency"

	"encore.dev/beta/errs"
)

func resetBalances() {
	mu.Lock()
	defer mu.Unlock()
	for k := range balances {
		delete(balances, k)
	}
}

func TestAddBalanceAndGetBalances(t *testing.T) {
	resetBalances()

	ctx := context.Background()
	err := AddBalance(ctx, &AddBalanceParams{
		Currency: currency.USD,
		Amount:   500,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	resp, err := GetBalances(ctx)
	if err != nil {
		t.Fatalf("expected no error from GetBalances, got %v", err)
	}

	got := resp.Balances[currency.USD]
	if got != 500 {
		t.Errorf("expected USD balance to be 500, got %d", got)
	}
}

func TestWithdraw_Success(t *testing.T) {
	resetBalances()

	ctx := context.Background()
	_ = AddBalance(ctx, &AddBalanceParams{
		Currency: currency.GEL,
		Amount:   200,
	})

	err := Withdraw(ctx, "GEL", WithdrawRequest{Amount: 100})
	if err != nil {
		t.Fatalf("expected successful withdrawal, got %v", err)
	}

	resp, _ := GetBalances(ctx)
	if resp.Balances[currency.GEL] != 100 {
		t.Errorf("expected GEL balance to be 100 after withdraw, got %d", resp.Balances[currency.GEL])
	}
}

func TestWithdraw_InsufficientFunds(t *testing.T) {
	resetBalances()

	ctx := context.Background()
	_ = AddBalance(ctx, &AddBalanceParams{Currency: currency.EUR, Amount: 50})

	err := Withdraw(ctx, "EUR", WithdrawRequest{Amount: 100})
	if err == nil {
		t.Fatal("expected error due to insufficient funds, got nil")
	}

	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.FailedPrecondition {
		t.Errorf("expected FailedPrecondition error, got %v", err)
	}
}

func TestAddBalance_InvalidAmount(t *testing.T) {
	resetBalances()

	ctx := context.Background()
	err := AddBalance(ctx, &AddBalanceParams{
		Currency: currency.USD,
		Amount:   0,
	})
	if err == nil {
		t.Fatal("expected error for zero amount, got nil")
	}
}
