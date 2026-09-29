// Contract for package idemkey (author-written; the implementer may not edit this file).
//
// idemkey derives the idempotency key for publishing one approved draft to a store, and
// decides whether the store's live copy already matches the draft.
//
//	var ErrEmptyDraftID = errors.New(...)  // draft id empty or only whitespace
//	var ErrBadDraftID   = errors.New(...)  // draft id contains ':'
//	var ErrNoFields     = errors.New(...)  // fields nil or empty
//
//	func Key(draftID string, fields map[string]string) (string, error)
//	    Returns draftID + ":" + the lowercase hex SHA-256 of the canonical encoding of fields.
//	    The canonical encoding is exactly what encoding/json.Marshal produces for the
//	    map[string]string (keys sorted, standard escaping). Values are hashed as given:
//	    no trimming, no case folding. Errors are checked in the order listed above
//	    (empty id, then ':' in the id, then no fields); on error the key is "".
//
//	func Matches(live, desired map[string]string) bool
//	    True when every key in desired is present in live and the two values are equal
//	    after strings.TrimSpace on both. Keys that exist only in live are ignored.
//	    An empty or nil desired matches nothing: it returns false.
package idemkey

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func want(t *testing.T, id string, fields map[string]string) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sum := sha256.Sum256(b)
	return id + ":" + hex.EncodeToString(sum[:])
}

func TestKeyIsDraftIDColonSHA256OfCanonicalJSON(t *testing.T) {
	f := map[string]string{"name": "Resistance Band Set", "regular_price": "24.95", "sku": "RB-01"}
	got, err := Key("draft-7", f)
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if got != want(t, "draft-7", f) {
		t.Fatalf("Key = %q, want %q", got, want(t, "draft-7", f))
	}
	if !strings.HasPrefix(got, "draft-7:") || len(got) != len("draft-7:")+64 {
		t.Fatalf("Key %q is not <id>:<64 hex chars>", got)
	}
	if strings.ToLower(got) != got {
		t.Fatalf("Key %q must be lowercase hex", got)
	}
}

func TestKeyIsStableUnderMapOrder(t *testing.T) {
	a := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
	first, err := Key("d", a)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		b := map[string]string{}
		for _, k := range []string{"e", "c", "a", "d", "b"} {
			b[k] = a[k]
		}
		got, err := Key("d", b)
		if err != nil || got != first {
			t.Fatalf("iteration %d: Key = %q, %v; want %q", i, got, err, first)
		}
	}
}

func TestKeyChangesWhenAnyValueChanges(t *testing.T) {
	base := map[string]string{"name": "Mat", "price": "10.00"}
	k1, _ := Key("d1", base)
	k2, _ := Key("d1", map[string]string{"name": "Mat", "price": "10.01"})
	k3, _ := Key("d2", base)
	if k1 == k2 {
		t.Fatal("a changed value must change the key")
	}
	if k1 == k3 {
		t.Fatal("a different draft id must change the key")
	}
}

func TestKeyKeepsFieldBoundariesUnambiguous(t *testing.T) {
	k1, _ := Key("d", map[string]string{"a": "b=c"})
	k2, _ := Key("d", map[string]string{"a=b": "c"})
	k3, _ := Key("d", map[string]string{"a": "b", "c": ""})
	k4, _ := Key("d", map[string]string{"a": "b\"c"})
	if k1 == k2 || k1 == k3 || k2 == k3 || k1 == k4 {
		t.Fatalf("keys collide across different field layouts: %q %q %q %q", k1, k2, k3, k4)
	}
}

func TestKeyHashesValuesAsGiven(t *testing.T) {
	k1, _ := Key("d", map[string]string{"name": "Mat"})
	k2, _ := Key("d", map[string]string{"name": " Mat "})
	k3, _ := Key("d", map[string]string{"name": "mat"})
	if k1 == k2 || k1 == k3 {
		t.Fatal("Key must not trim or fold case: the key records exactly what was approved")
	}
}

func TestKeyErrors(t *testing.T) {
	ok := map[string]string{"x": "1"}
	cases := []struct {
		name   string
		id     string
		fields map[string]string
		err    error
	}{
		{"empty id", "", ok, ErrEmptyDraftID},
		{"whitespace id", "  \t", ok, ErrEmptyDraftID},
		{"colon in id", "a:b", ok, ErrBadDraftID},
		{"nil fields", "d", nil, ErrNoFields},
		{"empty fields", "d", map[string]string{}, ErrNoFields},
		{"empty id wins over no fields", "", nil, ErrEmptyDraftID},
		{"colon wins over no fields", "a:b", nil, ErrBadDraftID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Key(c.id, c.fields)
			if !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if got != "" {
				t.Fatalf("key on error = %q, want empty", got)
			}
		})
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	if ErrEmptyDraftID == nil || ErrBadDraftID == nil || ErrNoFields == nil {
		t.Fatal("sentinels must be non-nil")
	}
	if errors.Is(ErrEmptyDraftID, ErrBadDraftID) || errors.Is(ErrBadDraftID, ErrNoFields) || errors.Is(ErrEmptyDraftID, ErrNoFields) {
		t.Fatal("sentinels must be distinct")
	}
}

func TestMatches(t *testing.T) {
	live := map[string]string{"name": "Mat ", "price": "10.00", "stock": "4", "extra": "ignored"}
	cases := []struct {
		name    string
		desired map[string]string
		want    bool
	}{
		{"all desired equal after trim", map[string]string{"name": "Mat", "price": " 10.00"}, true},
		{"extra live keys are ignored", map[string]string{"stock": "4"}, true},
		{"one value differs", map[string]string{"name": "Mat", "price": "10.01"}, false},
		{"desired key missing from live", map[string]string{"name": "Mat", "sku": "M-1"}, false},
		{"case differs", map[string]string{"name": "mat"}, false},
		{"empty desired matches nothing", map[string]string{}, false},
		{"nil desired matches nothing", nil, false},
		{"empty desired value vs missing live key", map[string]string{"sku": ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Matches(live, c.desired); got != c.want {
				t.Fatalf("Matches = %v, want %v", got, c.want)
			}
		})
	}
	if Matches(nil, map[string]string{"a": "1"}) {
		t.Fatal("nil live must not match a non-empty desired")
	}
}

func FuzzKeyStableUnderMapOrder(f *testing.F) {
	f.Add("draft-1", "name", "Mat", "price", "10.00")
	f.Add("d", "a", "b=c", "a=b", "c")
	f.Fuzz(func(t *testing.T, id, k1, v1, k2, v2 string) {
		if k1 == k2 {
			t.Skip("one key: the two maps legitimately differ")
		}
		m1 := map[string]string{k1: v1, k2: v2}
		m2 := map[string]string{k2: v2, k1: v1}
		a, errA := Key(id, m1)
		b, errB := Key(id, m2)
		if (errA == nil) != (errB == nil) || a != b {
			t.Fatalf("Key differs by insertion order: %q/%v vs %q/%v", a, errA, b, errB)
		}
	})
}
