// Package account provides an in-memory simulation of an account ledger DB,
// supporting crediting, withdrawing, and viewing balances for supported currencies.
// It is used by the billing service to add to the balance after successful bill charges.
//
// The in-memory storage is meant for demonstration and testing purposes only, we'd have a DB in a real app
package account

import (
	"context"
	"sync"

	"pave-fees-api/internal/currency"

	"encore.dev/beta/errs"
)

// balances holds the in-memory ledger: currency code -> balance.
// protected by mu for concurrent safety
var (
	mu       sync.Mutex
	balances = make(map[currency.Currency]int64)
)

type AddBalanceParams struct {
	Currency currency.Currency `json:"currency"`
	Amount   int64             `json:"amount"`
}

// called from billing service after a successfull bill workflow to add to the account balance
//
//encore:api private
func AddBalance(ctx context.Context, p *AddBalanceParams) error {
	if p.Amount == 0 {
		return &errs.Error{Code: errs.InvalidArgument, Message: "amount cannot be zero"}
	}
	mu.Lock()
	defer mu.Unlock()

	balances[p.Currency] += p.Amount
	return nil
}

type WithdrawRequest struct {
	Amount int64 `json:"amount"`
}

//encore:api public method=POST path=/balances/:curr/withdraw
func Withdraw(ctx context.Context, curr string, req WithdrawRequest) error {
	reqCur, err := currency.Parse(curr)
	if err != nil {
		return &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}

	if req.Amount <= 0 {
		return &errs.Error{Code: errs.InvalidArgument, Message: "amount must be > 0"}
	}
	mu.Lock()
	defer mu.Unlock()
	if balances[reqCur] < req.Amount {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "insufficient funds"}
	}
	balances[reqCur] -= req.Amount
	return nil
}

type BalancesResponse struct {
	Balances map[currency.Currency]int64 `json:"balances"`
}

//encore:api public method=GET path=/balances
func GetBalances(ctx context.Context) (BalancesResponse, error) {
	mu.Lock()
	defer mu.Unlock()

	out := make(map[currency.Currency]int64, len(currency.SupportedCurrencies))
	for _, cur := range currency.SupportedCurrencies {
		// balances[cur] will be 0 if cur is missing
		out[cur] = balances[cur]
	}

	return BalancesResponse{Balances: out}, nil
}
