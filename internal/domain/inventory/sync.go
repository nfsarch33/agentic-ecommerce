package inventory

// Item represents a single inventory record keyed by SKU.
type Item struct {
	SKU      string
	Quantity int
	Version  int
}

// UpdatePair holds the local and remote versions of a changed item.
type UpdatePair struct {
	Local  Item
	Remote Item
}

// ConflictPair holds two versions of an item that cannot be auto-resolved.
type ConflictPair struct {
	Local  Item
	Remote Item
}

// SyncPlan is the result of a reconciliation pass.
type SyncPlan struct {
	Additions []Item
	Removals  []Item
	Updates   []UpdatePair
	Conflicts []ConflictPair
}

// SyncEngine reconciles local and remote inventory.
type SyncEngine struct{}

func NewSyncEngine() *SyncEngine { return &SyncEngine{} }

// Reconcile computes the diff between local and remote inventory slices.
// Conflict: same SKU, same version, different quantity (concurrent edits).
// Update:   same SKU, remote version > local version.
func (e *SyncEngine) Reconcile(local, remote []Item) SyncPlan {
	localMap := toMap(local)
	remoteMap := toMap(remote)

	plan := SyncPlan{
		Additions: make([]Item, 0),
		Removals:  make([]Item, 0),
		Updates:   make([]UpdatePair, 0),
		Conflicts: make([]ConflictPair, 0),
	}

	for sku, rem := range remoteMap {
		loc, exists := localMap[sku]
		if !exists {
			plan.Additions = append(plan.Additions, rem)
			continue
		}
		if loc.Version == rem.Version && loc.Quantity == rem.Quantity {
			continue
		}
		if loc.Version == rem.Version && loc.Quantity != rem.Quantity {
			plan.Conflicts = append(plan.Conflicts, ConflictPair{Local: loc, Remote: rem})
			continue
		}
		if rem.Version > loc.Version {
			plan.Updates = append(plan.Updates, UpdatePair{Local: loc, Remote: rem})
		}
	}

	for sku := range localMap {
		if _, exists := remoteMap[sku]; !exists {
			plan.Removals = append(plan.Removals, localMap[sku])
		}
	}

	return plan
}

func toMap(items []Item) map[string]Item {
	m := make(map[string]Item, len(items))
	for _, it := range items {
		m[it.SKU] = it
	}
	return m
}
