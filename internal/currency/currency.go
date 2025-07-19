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
