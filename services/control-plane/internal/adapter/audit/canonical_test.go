package audit

// Coverage for canonicalJSON — the pure encoder driving the HMAC chain.
// The full Write / List / Verify paths require Postgres and live under
// the //go:build integration test file in this package.

import (
	"testing"
)

// TestCanonicalJSON_SortsKeysLexicographically — two maps with the same
// key/value pairs in different insert orders must produce byte-identical
// output. Determinism is what lets Verify reproduce the HMAC.
func TestCanonicalJSON_SortsKeysLexicographically(t *testing.T) {
	a := canonicalJSON(map[string]any{"b": 2, "a": 1})
	b := canonicalJSON(map[string]any{"a": 1, "b": 2})
	if string(a) != string(b) {
		t.Fatalf("encoding not deterministic: %s vs %s", a, b)
	}
	if string(a) != `{"a":1,"b":2}` {
		t.Fatalf("unexpected shape: %s", a)
	}
}

// TestCanonicalJSON_NestedAndUnicode — nested objects + escaped chars
// land verbatim in the output.
func TestCanonicalJSON_NestedAndUnicode(t *testing.T) {
	m := map[string]any{
		"name": "Iñtërnâtiônàlizætiøn",
		"meta": map[string]any{"key": "value"},
	}
	out := canonicalJSON(m)
	if string(out) == "" {
		t.Fatalf("empty encoding")
	}
	// Sorted keys: meta before name.
	if string(out)[:7] != `{"meta"` {
		t.Fatalf("meta should come first: %s", out)
	}
}

// TestCanonicalJSON_EmptyMap — an empty map encodes as "{}".
func TestCanonicalJSON_EmptyMap(t *testing.T) {
	if got := string(canonicalJSON(map[string]any{})); got != "{}" {
		t.Fatalf("empty: %q", got)
	}
}

// TestCanonicalJSON_NilMap — nil safely encodes to "{}".
func TestCanonicalJSON_NilMap(t *testing.T) {
	if got := string(canonicalJSON(nil)); got != "{}" {
		t.Fatalf("nil: %q", got)
	}
}

// TestCanonicalJSON_ValueTypes — numbers, booleans, nil, and arrays all
// land via the standard encoder. The test verifies the round-trip is
// stable across runs (Go's json.Marshal handles maps with sorted keys for
// nested values too).
func TestCanonicalJSON_ValueTypes(t *testing.T) {
	m := map[string]any{
		"num":   42,
		"flt":   1.5,
		"bool":  true,
		"null":  nil,
		"arr":   []any{1, 2, "three"},
		"str":   "hello",
	}
	got := string(canonicalJSON(m))
	expected := `{"arr":[1,2,"three"],"bool":true,"flt":1.5,"null":null,"num":42,"str":"hello"}`
	if got != expected {
		t.Fatalf("got %s\nwant %s", got, expected)
	}
}
