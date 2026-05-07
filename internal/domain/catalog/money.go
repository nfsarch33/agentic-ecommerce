package catalog

import (
	"errors"
	"strings"
)

var (
	ErrNegativeAmount  = errors.New("amount must be non-negative")
	ErrInvalidCurrency = errors.New("invalid ISO 4217 currency")
)

var supportedCurrencies = map[string]bool{
	"AUD": true,
	"USD": true,
	"GBP": true,
	"EUR": true,
}

type Money struct {
	amount   int
	currency string
}

func NewMoney(amount int, currency string) (Money, error) {
	if amount < 0 {
		return Money{}, ErrNegativeAmount
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !supportedCurrencies[currency] {
		return Money{}, ErrInvalidCurrency
	}
	return Money{amount: amount, currency: currency}, nil
}

func ZeroAUD() Money {
	return Money{amount: 0, currency: "AUD"}
}

func (m Money) Amount() int      { return m.amount }
func (m Money) Currency() string { return m.currency }
func (m Money) IsZero() bool     { return m.amount == 0 }
