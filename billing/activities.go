package billing

import (
	"context"
	"fmt"
	"time"

	"pave-fees-api/account"
	"pave-fees-api/internal/currency"
)

// simulates an tiem charge with a mocked fail case
func ChargeLineItemActivity(_ context.Context, li LineItem) error {
	time.Sleep(100 * time.Millisecond)
	if li.Name == "FAIL" {
		return fmt.Errorf("simulated failure for %s", li.ID)
	}
	return nil
}

// simulates an item refund
func RefundLineItemActivity(_ context.Context, li LineItem) error {
	time.Sleep(100 * time.Millisecond)
	return nil
}

// calls account service to add balance to the account after bill settlement
func CreditAccountActivity(ctx context.Context, amount int64, cur currency.Currency) error {
	return account.AddBalance(ctx, &account.AddBalanceParams{
		Currency: cur,
		Amount:   amount,
	})
}
