package data

import (
	"fmt"

	"pave-fees-api/internal/currency"
)

// accounts simulate the accounts table (id -> currency)
// in prod, this data would most likely be read from a ledger DB
var accounts = map[string]currency.Currency{
	"acc-usd": currency.USD,
	"acc-eur": currency.EUR,
	"acc-gel": currency.GEL,
}

// LookupAccount returns the canonical currency for the given account
func LookupAccount(id string) (currency.Currency, error) {
	cur, exists := accounts[id]
	if !exists {
		return "", fmt.Errorf("unknown account_id '%s'", id)
	}
	return cur, nil
}
