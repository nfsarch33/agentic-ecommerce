package product

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrVariantNotFound = errors.New("variant not found")
	ErrDuplicateSKU    = errors.New("duplicate SKU")
)

type Variant struct {
	SKU        string
	ProductID  string
	Attributes map[string]string
	Price      int
	Stock      int
}

type VariantManager struct {
	mu       sync.RWMutex
	variants map[string]Variant
}

func NewVariantManager() *VariantManager {
	return &VariantManager{variants: make(map[string]Variant)}
}

func (vm *VariantManager) Add(v Variant) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if _, exists := vm.variants[v.SKU]; exists {
		return ErrDuplicateSKU
	}
	vm.variants[v.SKU] = v
	return nil
}

func (vm *VariantManager) Get(sku string) (Variant, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	v, ok := vm.variants[sku]
	if !ok {
		return Variant{}, ErrVariantNotFound
	}
	return v, nil
}

func (vm *VariantManager) UpdateStock(sku string, stock int) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	v, ok := vm.variants[sku]
	if !ok {
		return ErrVariantNotFound
	}
	v.Stock = stock
	vm.variants[sku] = v
	return nil
}

func (vm *VariantManager) ListByProduct(productID string) []Variant {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	var result []Variant
	for _, v := range vm.variants {
		if v.ProductID == productID {
			result = append(result, v)
		}
	}
	return result
}

// GenerateCombinations returns all attribute value combinations.
func (vm *VariantManager) GenerateCombinations(attributes map[string][]string) []map[string]string {
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return cartesianProduct(keys, attributes)
}

func cartesianProduct(keys []string, attributes map[string][]string) []map[string]string {
	if len(keys) == 0 {
		return []map[string]string{{}}
	}
	key := keys[0]
	rest := cartesianProduct(keys[1:], attributes)
	var result []map[string]string
	for _, val := range attributes[key] {
		for _, combo := range rest {
			merged := make(map[string]string, len(combo)+1)
			for k, v := range combo {
				merged[k] = v
			}
			merged[key] = val
			result = append(result, merged)
		}
	}
	return result
}

// GenerateSKU creates a deterministic SKU from a base and attribute map.
func (vm *VariantManager) GenerateSKU(base string, attributes map[string]string) string {
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(attributes))
	for _, k := range keys {
		parts = append(parts, strings.ToUpper(fmt.Sprintf("%s%s", string([]rune(k)[:1]), attributes[k])))
	}
	return base + "-" + strings.Join(parts, "-")
}
