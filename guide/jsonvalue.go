package guide

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// jsonValue is a parsed JSON value that preserves object key insertion order.
// It is one of: *jsonObject, []jsonValue, string, json.Number, bool, or nullValue.
type jsonValue = any

// nullValue is JSON null, kept distinct from a Go nil so it round-trips.
type nullValue struct{}

// jsonObject is a JSON object that remembers the order its keys first appeared.
type jsonObject struct {
	keys []string
	vals map[string]jsonValue
}

func newJSONObject() *jsonObject {
	return &jsonObject{vals: map[string]jsonValue{}}
}

// set records key -> val, appending the key on first sight (first occurrence
// fixes position) and letting a later value overwrite in place.
func (o *jsonObject) set(key string, val jsonValue) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

// LintJSON validates that body is a single well-formed JSON object with no
// trailing content, tolerating {{token}} placeholders by treating each as a
// neutral scalar (a real substitution runs at compose time).
func LintJSON(body []byte) error {
	_, err := parseJSONObject(tokenRe.ReplaceAll(body, []byte("0")))
	return err
}

// parseJSONObject strictly parses data as a single JSON object value with no
// trailing content, preserving key order.
func parseJSONObject(data []byte) (*jsonObject, error) {
	v, err := parseJSONValue(data)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(*jsonObject)
	if !ok {
		return nil, ErrJSONNotObject
	}
	return obj, nil
}

// parseJSONValue parses data as one JSON value, rejecting trailing content.
func parseJSONValue(data []byte) (jsonValue, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONParse, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: unexpected trailing data", ErrJSONParse)
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (jsonValue, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return parseObject(dec)
		case '[':
			return parseArray(dec)
		default:
			return nil, fmt.Errorf("unexpected %q", t)
		}
	case string:
		return t, nil
	case json.Number:
		return t, nil
	case bool:
		return t, nil
	case nil:
		return nullValue{}, nil
	default:
		return nil, fmt.Errorf("unexpected token %v", tok)
	}
}

func parseObject(dec *json.Decoder) (*jsonObject, error) {
	obj := newJSONObject()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is not a string: %v", keyTok)
		}
		val, err := parseValue(dec)
		if err != nil {
			return nil, err
		}
		obj.set(key, val)
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return nil, err
	}
	return obj, nil
}

func parseArray(dec *json.Decoder) ([]jsonValue, error) {
	arr := []jsonValue{}
	for dec.More() {
		val, err := parseValue(dec)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	if _, err := dec.Token(); err != nil { // consume ']'
		return nil, err
	}
	return arr, nil
}

// encodeJSON serializes v as stable, pretty JSON: 2-space indent, no HTML
// escaping, LF newlines, and a single trailing newline.
func encodeJSON(v jsonValue) ([]byte, error) {
	var b bytes.Buffer
	if err := encodeValue(&b, v, 0); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

func encodeValue(b *bytes.Buffer, v jsonValue, depth int) error {
	switch t := v.(type) {
	case *jsonObject:
		return encodeObject(b, t, depth)
	case []jsonValue:
		return encodeArray(b, t, depth)
	case nullValue:
		b.WriteString("null")
		return nil
	case string:
		return encodeString(b, t)
	case json.Number:
		b.WriteString(string(t))
		return nil
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return nil
	default:
		return fmt.Errorf("cannot encode JSON value of type %T", v)
	}
}

func encodeObject(b *bytes.Buffer, obj *jsonObject, depth int) error {
	if len(obj.keys) == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	inner := depth + 1
	for i, k := range obj.keys {
		writeIndent(b, inner)
		if err := encodeString(b, k); err != nil {
			return err
		}
		b.WriteString(": ")
		if err := encodeValue(b, obj.vals[k], inner); err != nil {
			return err
		}
		if i < len(obj.keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	writeIndent(b, depth)
	b.WriteByte('}')
	return nil
}

func encodeArray(b *bytes.Buffer, arr []jsonValue, depth int) error {
	if len(arr) == 0 {
		b.WriteString("[]")
		return nil
	}
	b.WriteString("[\n")
	inner := depth + 1
	for i, v := range arr {
		writeIndent(b, inner)
		if err := encodeValue(b, v, inner); err != nil {
			return err
		}
		if i < len(arr)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	writeIndent(b, depth)
	b.WriteByte(']')
	return nil
}

// encodeString writes s as a JSON string with HTML escaping disabled, so `<`,
// `>`, and `&` survive verbatim.
func encodeString(b *bytes.Buffer, s string) error {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	b.Write(bytes.TrimRight(tmp.Bytes(), "\n"))
	return nil
}

func writeIndent(b *bytes.Buffer, depth int) {
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
}
