package provider

import (
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// unwrapDynamic returns the concrete attr.Value a types.Dynamic wraps. If v
// is not a types.Dynamic, it is returned unchanged (so callers can safely
// pass either a still-wrapped or already-unwrapped value). A null Dynamic's
// UnderlyingValue() is nil, so in that case the Dynamic itself is returned
// (its IsNull() reports the null-ness) rather than a bare nil interface.
func unwrapDynamic(t *testing.T, v attr.Value) attr.Value {
	t.Helper()
	dyn, ok := v.(types.Dynamic)
	if !ok {
		return v
	}
	if dyn.IsNull() || dyn.IsUnknown() {
		return dyn
	}
	return dyn.UnderlyingValue()
}

// objectAttr unwraps v as an object and returns the named attribute's
// underlying (unwrapped) value.
func objectAttr(t *testing.T, v attr.Value, name string) attr.Value {
	t.Helper()
	obj, ok := unwrapDynamic(t, v).(types.Object)
	if !ok {
		t.Fatalf("value is not types.Object: %T (%v)", v, v)
	}
	attrs := obj.Attributes()
	child, ok := attrs[name]
	if !ok {
		t.Fatalf("object has no attribute %q; attributes: %v", name, attrs)
	}
	return unwrapDynamic(t, child)
}

// tupleElem unwraps v as a tuple and returns the element at index i
// (unwrapped).
func tupleElem(t *testing.T, v attr.Value, i int) attr.Value {
	t.Helper()
	tuple, ok := unwrapDynamic(t, v).(types.Tuple)
	if !ok {
		t.Fatalf("value is not types.Tuple: %T (%v)", v, v)
	}
	elems := tuple.Elements()
	if i < 0 || i >= len(elems) {
		t.Fatalf("tuple index %d out of range (len %d)", i, len(elems))
	}
	return unwrapDynamic(t, elems[i])
}

func wantString(t *testing.T, v attr.Value, want string) {
	t.Helper()
	s, ok := v.(types.String)
	if !ok {
		t.Fatalf("value is not types.String: %T (%v)", v, v)
	}
	if got := s.ValueString(); got != want {
		t.Errorf("got string %q, want %q", got, want)
	}
}

func wantBool(t *testing.T, v attr.Value, want bool) {
	t.Helper()
	b, ok := v.(types.Bool)
	if !ok {
		t.Fatalf("value is not types.Bool: %T (%v)", v, v)
	}
	if got := b.ValueBool(); got != want {
		t.Errorf("got bool %v, want %v", got, want)
	}
}

func wantNull(t *testing.T, v attr.Value) {
	t.Helper()
	if !v.IsNull() {
		t.Errorf("value is not null: %T (%v)", v, v)
	}
}

// wantNumberString asserts that v is a types.Number whose exact decimal text
// (via *big.Float.Text) equals want. Comparing decimal text, rather than
// converting to float64, is what actually proves precision was preserved.
func wantNumberString(t *testing.T, v attr.Value, want string) {
	t.Helper()
	n, ok := v.(types.Number)
	if !ok {
		t.Fatalf("value is not types.Number: %T (%v)", v, v)
	}
	f := n.ValueBigFloat()
	if f == nil {
		t.Fatalf("number value is nil")
	}
	got := f.Text('f', -1)
	if got != want {
		t.Errorf("got number %q, want %q", got, want)
	}
}

func TestDecodeJSONC(t *testing.T) {
	t.Run("plain JSON, no comments", func(t *testing.T) {
		v, err := decodeJSONC(`{"name": "plain", "value": 1}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantString(t, objectAttr(t, v, "name"), "plain")
		wantNumberString(t, objectAttr(t, v, "value"), "1")
	})

	t.Run("line comments", func(t *testing.T) {
		content := `{
  // this is a line comment
  "name": "line-comments"
}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantString(t, objectAttr(t, v, "name"), "line-comments")
	})

	t.Run("block comments including multi-line", func(t *testing.T) {
		content := `{
  /* single line block comment */
  "name": "block-comments",
  /*
   * multi-line
   * block comment
   */
  "value": 2
}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantString(t, objectAttr(t, v, "name"), "block-comments")
		wantNumberString(t, objectAttr(t, v, "value"), "2")
	})

	t.Run("trailing commas in objects and arrays", func(t *testing.T) {
		content := `{
  "list": [1, 2, 3,],
  "name": "trailing-commas",
}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantString(t, objectAttr(t, v, "name"), "trailing-commas")
		list := objectAttr(t, v, "list")
		wantNumberString(t, tupleElem(t, list, 0), "1")
		wantNumberString(t, tupleElem(t, list, 1), "2")
		wantNumberString(t, tupleElem(t, list, 2), "3")
	})

	t.Run("string containing comment-like and comma-like substrings is preserved verbatim", func(t *testing.T) {
		content := `{"url": "https://example.com/a,b // not a comment /* still not */ ,"}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantString(t, objectAttr(t, v, "url"), "https://example.com/a,b // not a comment /* still not */ ,")
	})

	t.Run("escaped quotes inside a string near a comment-looking substring", func(t *testing.T) {
		content := `{"note": "a \"quoted\" // not a comment"}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantString(t, objectAttr(t, v, "note"), `a "quoted" // not a comment`)
	})

	t.Run("invalid content that is still broken JSON even after stripping", func(t *testing.T) {
		_, err := decodeJSONC(`{ "name": "unterminated", `)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("nested objects and arrays", func(t *testing.T) {
		content := `{
  "items": [
    {"id": 1, "tags": ["a", "b"]},
    {"id": 2, "tags": ["c"]}
  ]
}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := objectAttr(t, v, "items")
		first := tupleElem(t, items, 0)
		wantNumberString(t, objectAttr(t, first, "id"), "1")
		firstTags := objectAttr(t, first, "tags")
		wantString(t, tupleElem(t, firstTags, 0), "a")
		wantString(t, tupleElem(t, firstTags, 1), "b")

		second := tupleElem(t, items, 1)
		wantNumberString(t, objectAttr(t, second, "id"), "2")
		secondTags := objectAttr(t, second, "tags")
		wantString(t, tupleElem(t, secondTags, 0), "c")
	})

	t.Run("booleans and null", func(t *testing.T) {
		content := `{"enabled": true, "disabled": false, "missing": null}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantBool(t, objectAttr(t, v, "enabled"), true)
		wantBool(t, objectAttr(t, v, "disabled"), false)
		wantNull(t, objectAttr(t, v, "missing"))
	})

	t.Run("large integer beyond float64 precision is preserved exactly", func(t *testing.T) {
		content := `{"big": 9007199254740993}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantNumberString(t, objectAttr(t, v, "big"), "9007199254740993")

		// Sanity check: confirm this value really does lose precision as a
		// float64, so the test is actually proving something.
		asFloat, _, _ := new(big.Float).SetPrec(512).Parse("9007199254740993", 10)
		f64, _ := asFloat.Float64()
		if int64(f64) == 9007199254740993 {
			t.Fatalf("test fixture does not actually exceed float64 precision")
		}
	})

	t.Run("realistic wrangler.jsonc-like fixture", func(t *testing.T) {
		content := `{
  // Worker name, must be globally unique within the account
  "name": "my-worker",
  "main": "src/index.ts",
  /*
   * compatibility_date pins the Workers runtime behavior
   * to a specific date so future runtime changes don't
   * silently change behavior.
   */
  "compatibility_date": "2026-01-01",
  "vars": {
    "FOO": "bar",
  },
}`
		v, err := decodeJSONC(content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantString(t, objectAttr(t, v, "name"), "my-worker")
		wantString(t, objectAttr(t, v, "main"), "src/index.ts")
		wantString(t, objectAttr(t, v, "compatibility_date"), "2026-01-01")
		vars := objectAttr(t, v, "vars")
		wantString(t, objectAttr(t, vars, "FOO"), "bar")
	})

	t.Run("trailing garbage after the top-level value is rejected", func(t *testing.T) {
		_, err := decodeJSONC(`{"name": "a"} {"name": "b"}`)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}
