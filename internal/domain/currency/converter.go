package currency

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var ErrUnknownCurrency = errors.New("unknown currency or exchange rate")

// Money is a value object representing an amount in minor units (cents/pence/etc).
type Money struct {
	Amount   int64
	Currency string
}

// ExchangeRateStore retrieves exchange rates between currency pairs.
type ExchangeRateStore interface {
	GetRate(from, to string) (float64, error)
}

// InMemoryRateStore holds rates as "FROM/TO" -> rate.
type InMemoryRateStore struct {
	rates map[string]float64
}

func NewInMemoryRateStore(rates map[string]float64) *InMemoryRateStore {
	r := make(map[string]float64, len(rates))
	for k, v := range rates {
		r[strings.ToUpper(k)] = v
	}
	return &InMemoryRateStore{rates: r}
}

func (s *InMemoryRateStore) GetRate(from, to string) (float64, error) {
	key := strings.ToUpper(from) + "/" + strings.ToUpper(to)
	if rate, ok := s.rates[key]; ok {
		return rate, nil
	}
	return 0, ErrUnknownCurrency
}

// CurrencyConverter converts Money between currencies.
type CurrencyConverter struct {
	store ExchangeRateStore
}

func NewCurrencyConverter(store ExchangeRateStore) *CurrencyConverter {
	return &CurrencyConverter{store: store}
}

// Convert converts m to the target currency using the registered rate.
func (c *CurrencyConverter) Convert(m Money, target string) (Money, error) {
	if strings.EqualFold(m.Currency, target) {
		return Money{Amount: m.Amount, Currency: strings.ToUpper(target)}, nil
	}
	rate, err := c.store.GetRate(m.Currency, target)
	if err != nil {
		return Money{}, fmt.Errorf("convert %s to %s: %w", m.Currency, target, err)
	}
	converted := int64(math.Round(float64(m.Amount) * rate))
	return Money{Amount: converted, Currency: strings.ToUpper(target)}, nil
}

// FormatDisplay formats Money for human display with currency symbol and separators.
func FormatDisplay(m Money) string {
	switch strings.ToUpper(m.Currency) {
	case "JPY", "KRW", "VND":
		return currencySymbol(m.Currency) + formatNoDecimal(m.Amount)
	default:
		return currencySymbol(m.Currency) + formatWithDecimal(m.Amount)
	}
}

func currencySymbol(currency string) string {
	switch strings.ToUpper(currency) {
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "AUD":
		return "A$"
	case "JPY":
		return "¥"
	case "KRW":
		return "₩"
	default:
		return strings.ToUpper(currency) + " "
	}
}

func formatWithDecimal(cents int64) string {
	major := cents / 100
	minor := cents % 100
	return fmt.Sprintf("%s.%02d", formatThousands(major), minor)
}

func formatNoDecimal(units int64) string {
	return formatThousands(units)
}

func formatThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	var result []byte
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
	}
	return string(result)
}
