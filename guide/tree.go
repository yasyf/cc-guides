package guide

import "encoding/json"

// treeValue is a parsed value in a format-neutral, order-preserving tree — the shared
// representation the merge engine folds over, independent of the codec that produced it.
// It is one of: *treeObject, []treeValue, string, json.Number, bool, or nullValue.
type treeValue = any

// nullValue is a null leaf, kept distinct from a Go nil so it round-trips.
type nullValue struct{}

// treeObject is an object that remembers the order its keys first appeared.
type treeObject struct {
	keys []string
	vals map[string]treeValue
}

func newTreeObject() *treeObject {
	return &treeObject{vals: map[string]treeValue{}}
}

// set records key -> val, appending the key on first sight (first occurrence
// fixes position) and letting a later value overwrite in place.
func (o *treeObject) set(key string, val treeValue) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

// mergeValues deep-merges src into dst: two objects merge recursively, two arrays
// union with structural-equality dedupe, and any scalar or type conflict is
// later-wins (src).
func mergeValues(dst, src treeValue) treeValue {
	if dstObj, ok := dst.(*treeObject); ok {
		if srcObj, ok := src.(*treeObject); ok {
			return mergeObjects(dstObj, srcObj)
		}
	}
	if dstArr, ok := dst.([]treeValue); ok {
		if srcArr, ok := src.([]treeValue); ok {
			return mergeArrays(dstArr, srcArr)
		}
	}
	return src
}

// mergeObjects merges src into dst in place: a shared key merges recursively (its
// position stays at dst's first occurrence); a new key is appended.
func mergeObjects(dst, src *treeObject) *treeObject {
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
func mergeArrays(dst, src []treeValue) []treeValue {
	out := append([]treeValue(nil), dst...)
	for _, sv := range src {
		if !containsValue(out, sv) {
			out = append(out, sv)
		}
	}
	return out
}

func containsValue(arr []treeValue, v treeValue) bool {
	for _, e := range arr {
		if equalValues(e, v) {
			return true
		}
	}
	return false
}

// equalValues reports deep structural equality, order-insensitive for object keys.
func equalValues(a, b treeValue) bool {
	switch av := a.(type) {
	case *treeObject:
		bv, ok := b.(*treeObject)
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
	case []treeValue:
		bv, ok := b.([]treeValue)
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
