// Package sku normalises product SKUs and builds URL slugs from product
// titles, the two identifier spellings a WooCommerce ops toolkit handles.
package sku

import (
	"errors"
	"strings"
)

// ErrEmpty is returned by NormaliseSKU and Slug when the sanitised result
// would be the empty string.
var ErrEmpty = errors.New("sku: empty")

// NormaliseSKU returns the canonical SKU form of raw: surrounding whitespace
// is trimmed, ASCII letters uppercased, every byte outside [A-Za-z0-9-]
// dropped, runs of '-' collapsed and any leading or trailing '-' stripped.
// It returns ErrEmpty when the result is empty.
func NormaliseSKU(raw string) (string, error) {
	return sanitise(strings.TrimSpace(raw), true)
}

// Slug returns the URL slug form of raw: surrounding whitespace is trimmed,
// ASCII letters lowercased, spaces converted to '-' (BEFORE the character
// filter, so "Red Widget XL" becomes "red-widget-xl"), every other byte
// outside [A-Za-z0-9-] dropped, runs of '-' collapsed and any leading or
// trailing '-' stripped. It returns ErrEmpty when the result is empty.
func Slug(raw string) (string, error) {
	return sanitise(strings.TrimSpace(raw), false)
}

// sanitise runs the pipeline shared by NormaliseSKU and Slug. When upper is
// true ASCII letters are uppercased (NormaliseSKU); otherwise they are
// lowercased (Slug). In Slug mode a space is emitted as '-' before the
// [A-Za-z0-9-] filter; in NormaliseSKU mode a space is simply dropped.
func sanitise(raw string, upper bool) (string, error) {
	if raw == "" {
		return "", ErrEmpty
	}
	var b []byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == ' ' {
			if !upper {
				b = append(b, '-')
			}
			continue
		}
		switch {
		case upper && c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		case !upper && c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		}
		if isAllowed(c) {
			b = append(b, c)
		}
	}
	out := collapseHyphens(b)
	if out == "" {
		return "", ErrEmpty
	}
	return out, nil
}

// isAllowed reports whether c is one of the bytes that survive the filter:
// an ASCII letter, digit or '-'.
func isAllowed(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-':
		return true
	}
	return false
}

// collapseHyphens collapses consecutive '-' bytes to one, then strips any
// leading and trailing '-'.
func collapseHyphens(b []byte) string {
	out := make([]byte, 0, len(b))
	prevHyphen := false
	for _, c := range b {
		if c == '-' {
			if prevHyphen {
				continue
			}
			prevHyphen = true
		} else {
			prevHyphen = false
		}
		out = append(out, c)
	}
	for len(out) > 0 && out[0] == '-' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
