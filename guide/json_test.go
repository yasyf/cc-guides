package guide_test

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

func jp(body string) guide.Piece {
	return guide.Piece{Body: []byte(body), Origin: "test.json"}
}

func argPiece(body string, args map[string]string) guide.Piece {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return guide.Piece{Body: []byte(body), Args: args, Keys: keys, Origin: "arg.json"}
}

func composeJSON(t *testing.T, pieces ...guide.Piece) string {
	t.Helper()
	out, err := guide.Compose(guide.KindJSON, pieces)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	return string(out)
}

// Deep object merge, array union with dedupe, and stable 2-space encoding.
func TestComposeJSONDeepMerge(t *testing.T) {
	got := composeJSON(
		t,
		jp(`{"a": {"x": 1}, "b": [1, 2]}`),
		jp(`{"a": {"y": 2}, "b": [2, 3], "c": true}`),
	)
	want := "{\n" +
		"  \"a\": {\n" +
		"    \"x\": 1,\n" +
		"    \"y\": 2\n" +
		"  },\n" +
		"  \"b\": [\n" +
		"    1,\n" +
		"    2,\n" +
		"    3\n" +
		"  ],\n" +
		"  \"c\": true\n" +
		"}\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A later scalar or a type conflict is later-wins.
func TestComposeJSONLaterWins(t *testing.T) {
	if got := composeJSON(t, jp(`{"k": "old"}`), jp(`{"k": "new"}`)); got != "{\n  \"k\": \"new\"\n}\n" {
		t.Fatalf("scalar later-wins: %q", got)
	}
	// Object replaced by an array (type conflict) — later wins wholesale.
	got := composeJSON(t, jp(`{"k": {"a": 1}}`), jp(`{"k": [1]}`))
	if got != "{\n  \"k\": [\n    1\n  ]\n}\n" {
		t.Fatalf("type-conflict later-wins: %q", got)
	}
}

// Array union dedupes objects by key-order-insensitive structural equality.
func TestComposeJSONArrayObjectEquality(t *testing.T) {
	got := composeJSON(
		t,
		jp(`{"arr": [{"a": 1, "b": 2}]}`),
		jp(`{"arr": [{"b": 2, "a": 1}, {"c": 3}]}`),
	)
	// The reordered duplicate is dropped; {"c":3} is appended.
	want := "{\n  \"arr\": [\n    {\n      \"a\": 1,\n      \"b\": 2\n    },\n    {\n      \"c\": 3\n    }\n  ]\n}\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Keys keep first-occurrence insertion order across fragments; never sorted.
func TestComposeJSONInsertionOrder(t *testing.T) {
	got := composeJSON(t, jp(`{"z": 1, "a": 2}`), jp(`{"m": 3, "a": 9}`))
	want := "{\n  \"z\": 1,\n  \"a\": 9,\n  \"m\": 3\n}\n"
	if got != want {
		t.Fatalf("insertion order not preserved:\n%s", got)
	}
}

// HTML metacharacters survive verbatim (no &lt; escaping).
func TestComposeJSONNoHTMLEscape(t *testing.T) {
	got := composeJSON(t, jp(`{"k": "a<b>&c"}`))
	if !strings.Contains(got, `"a<b>&c"`) {
		t.Fatalf("HTML-escaped:\n%s", got)
	}
}

func TestComposeJSONErrors(t *testing.T) {
	if _, err := guide.Compose(guide.KindJSON, []guide.Piece{jp(`[1, 2]`)}); !errors.Is(err, guide.ErrJSONNotObject) {
		t.Fatalf("array root err = %v, want ErrJSONNotObject", err)
	}
	if _, err := guide.Compose(guide.KindJSON, []guide.Piece{jp(`{"a": 1} garbage`)}); !errors.Is(err, guide.ErrJSONParse) {
		t.Fatalf("trailing-data err = %v, want ErrJSONParse", err)
	}
	if _, err := guide.Compose(guide.KindJSON, []guide.Piece{jp("{\"a\": 1}\r\n")}); !errors.Is(err, guide.ErrCRLF) {
		t.Fatalf("CRLF err = %v, want ErrCRLF", err)
	}
}

// Args substitute on raw text first, with the same two-way strictness as md/sh.
func TestComposeJSONTokens(t *testing.T) {
	got := composeJSON(t, argPiece(`{"python": "{{venv}}/bin/python"}`, map[string]string{"venv": ".venv"}))
	if got != "{\n  \"python\": \".venv/bin/python\"\n}\n" {
		t.Fatalf("token substitution: %q", got)
	}
	// A missing token arg is a hard error.
	if _, err := guide.Compose(guide.KindJSON, []guide.Piece{argPiece(`{"a": "{{missing}}"}`, map[string]string{})}); !errors.Is(err, guide.ErrTokenNoArg) {
		t.Fatalf("missing-arg err = %v, want ErrTokenNoArg", err)
	}
	// An unused arg is a hard error.
	if _, err := guide.Compose(guide.KindJSON, []guide.Piece{argPiece(`{"a": 1}`, map[string]string{"unused": "x"})}); !errors.Is(err, guide.ErrArgUnused) {
		t.Fatalf("unused-arg err = %v, want ErrArgUnused", err)
	}
	// A nil-Args piece is never scanned: a literal {{x}} inside a string survives.
	if got := composeJSON(t, jp(`{"a": "{{x}}"}`)); got != "{\n  \"a\": \"{{x}}\"\n}\n" {
		t.Fatalf("nil-args literal token: %q", got)
	}
}
