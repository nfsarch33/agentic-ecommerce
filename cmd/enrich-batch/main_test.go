package main

import "testing"

// MUTANT: make publishGate return nil unconditionally and this goes red —
// the batch would believe it may write to the store with no proxy named,
// which is the "nothing published without an approval row" contract's first
// door (the gate's ErrNoApproval is the second; pinned in publishgate).
func TestPublishRefusedWithoutAuditedProxy(t *testing.T) {
	if err := publishGate(true, ""); err == nil {
		t.Fatal("publish mode with no store proxy named was allowed: the store must be written only through the audited proxy")
	}
	if err := publishGate(false, ""); err != nil {
		t.Fatalf("draft mode must always run: %v", err)
	}
	if err := publishGate(true, "http://127.0.0.1:8092"); err != nil {
		t.Fatalf("publish mode with the proxy named should pass the first door: %v", err)
	}
}

func TestParseJSONObjectStripsFences(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1}`:                `{"a":1}`,
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"prose {\"a\":1} more":    `{"a":1}`,
	} {
		got, err := parseJSONObject(in)
		if err != nil {
			t.Fatalf("parse %q: %v", in, err)
		}
		if string(got) != want {
			t.Fatalf("parse %q = %s, want %s", in, got, want)
		}
	}
	if _, err := parseJSONObject("no json here"); err == nil {
		t.Fatal("non-JSON reply parsed")
	}
}
