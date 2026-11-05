// Package strictjson decodes JSON without silently accepting duplicate keys.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxDepth = 32

func Object(data []byte) (map[string]any, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	result, err := value(dec, 0)
	if err != nil {
		return nil, err
	}
	obj, ok := result.(map[string]any)
	if !ok {
		return nil, errors.New("top-level JSON value must be an object")
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON tokens")
		}
		return nil, err
	}
	return obj, nil
}

func value(dec *json.Decoder, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, errors.New("maximum JSON nesting exceeded")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		obj := make(map[string]any)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("invalid object key")
			}
			if _, exists := obj[key]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", key)
			}
			item, err := value(dec, depth+1)
			if err != nil {
				return nil, err
			}
			obj[key] = item
		}
		_, err := dec.Token()
		return obj, err
	case json.Delim('['):
		var items []any
		for dec.More() {
			item, err := value(dec, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		_, err := dec.Token()
		return items, err
	default:
		if d, ok := tok.(json.Delim); ok {
			return nil, fmt.Errorf("unexpected JSON delimiter %q", d)
		}
		return tok, nil
	}
}
