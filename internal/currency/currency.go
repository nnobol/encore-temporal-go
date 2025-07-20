// Package currency provides type-safe handling and validation
// of supported currency codes in the billing system.
package currency

import (
	"fmt"
	"strings"
)

type Currency string

// could be replaced with ISO‑4217 codes for a real-world app
const (
	USD Currency = "USD"
	EUR Currency = "EUR"
	GEL Currency = "GEL"
)

// used in account service handler to zero out the balances in the response
var SupportedCurrencies = []Currency{
	USD,
	EUR,
	GEL,
}

// ParseCurrency converts the input currency string to a canonical Currency type in a case insensitive way
func Parse(raw string) (Currency, error) {
	s := strings.ToUpper(raw)
	switch Currency(s) {
	case USD, EUR, GEL:
		return Currency(s), nil
	default:
		return "", fmt.Errorf("unsupported currency '%s'", raw)
	}
}
