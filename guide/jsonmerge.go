package guide

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ComposeJSON composes JSON fragment pieces into one artifact body: token
// substitution on raw text first (only for arg-declaring pieces, two-way strict,
// exactly as Compose), then strict object-root parse, then deep merge in fragment
// order, then stable encoding. CRLF anywhere is a hard error.
func ComposeJSON(pieces []Piece) ([]byte, error) {
	var acc *jsonObject
	for _, p := range pieces {
		if bytes.IndexByte(p.Body, '\r') >= 0 {
			return nil, fmt.Errorf("%w: %s", ErrCRLF, p.Origin)
		}
		text := string(p.Body)
		if p.Args != nil {
			sub, err := substituteTokens(text, p.Args, p.Keys, p.Origin)
			if err != nil {
				return nil, err
			}
			text = sub
		}
		obj, err := parseJSONObject([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Origin, err)
		}
		if acc == nil {
			acc = obj
		} else {
			acc = mergeObjects(acc, obj)
		}
	}
	if acc == nil {
		acc = newJSONObject()
	}
	return encodeJSON(acc)
}

// mergeValues deep-merges src into dst: two objects merge recursively, two arrays
// union with structural-equality dedupe, and any scalar or type conflict is
// later-wins (src).
func mergeValues(dst, src jsonValue) jsonValue {
	if dstObj, ok := dst.(*jsonObject); ok {
		if srcObj, ok := src.(*jsonObject); ok {
			return mergeObjects(dstObj, srcObj)
		}
	}
	if dstArr, ok := dst.([]jsonValue); ok {
		if srcArr, ok := src.([]jsonValue); ok {
			return mergeArrays(dstArr, srcArr)
		}
	}
	return src
}

// mergeObjects merges src into dst in place: a shared key merges recursively (its
// position stays at dst's first occurrence); a new key is appended.
func mergeObjects(dst, src *jsonObject) *jsonObject {
	for _, k := range src.keys {
		sv := src.vals[k]
		if dv, ok := dst.vals[k]; ok {
			dst.vals[k] = mergeValues(dv, sv)
		} else {
			dst.set(k, sv)
		}
	}
	return dst
}

// mergeArrays returns dst followed by every src element not already present by
// deep structural equality — a union that preserves order and never sorts.
func mergeArrays(dst, src []jsonValue) []jsonValue {
	out := append([]jsonValue(nil), dst...)
	for _, sv := range src {
		if !containsValue(out, sv) {
			out = append(out, sv)
		}
	}
	return out
}

func containsValue(arr []jsonValue, v jsonValue) bool {
	for _, e := range arr {
		if equalValues(e, v) {
			return true
		}
	}
	return false
}

// equalValues reports deep structural equality, order-insensitive for object keys.
func equalValues(a, b jsonValue) bool {
	switch av := a.(type) {
	case *jsonObject:
		bv, ok := b.(*jsonObject)
		if !ok || len(av.keys) != len(bv.keys) {
			return false
		}
		for _, k := range av.keys {
			bval, ok := bv.vals[k]
			if !ok || !equalValues(av.vals[k], bval) {
				return false
			}
		}
		return true
	case []jsonValue:
		bv, ok := b.([]jsonValue)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalValues(av[i], bv[i]) {
				return false
			}
		}
		return true
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case json.Number:
		bv, ok := b.(json.Number)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nullValue:
		_, ok := b.(nullValue)
		return ok
	default:
		return false
	}
}
