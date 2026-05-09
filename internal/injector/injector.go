package injector

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Injector knows how to add a set of fields to a request body. The actual
// content-type detection lives in the caller; the injector is told what kind
// of body it has via the constructor functions below.

type Kind int

const (
	KindUnknown Kind = iota
	KindJSON
	KindForm
)

// DetectKind returns the body kind based on a Content-Type string.
func DetectKind(ct string) Kind {
	ct = strings.ToLower(strings.TrimSpace(ct))
	switch {
	case strings.Contains(ct, "application/json"):
		return KindJSON
	case strings.Contains(ct, "application/x-www-form-urlencoded"):
		return KindForm
	default:
		return KindUnknown
	}
}

// InjectJSON parses body, then recursively walks the resulting structure and
// injects each (field, value) pair into:
//   - every object encountered (at every depth);
//   - the FIRST object element of any array encountered.
//
// At any given object level a field that already exists is skipped — but the
// same field may still be injected at a different level where it is absent.
//
// The boolean return reports whether AT LEAST ONE field was actually
// inserted somewhere in the body. If false, the produced body is identical
// to the input and the caller can skip sending the request.
func InjectJSON(body []byte, fields []string, value interface{}) ([]byte, bool, error) {
	if len(fields) == 0 {
		return body, false, nil
	}

	var doc interface{}
	if len(body) == 0 {
		doc = map[string]interface{}{}
	} else if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, fmt.Errorf("body is not valid JSON: %w", err)
	}

	injected := false
	doc = walk(doc, fields, value, true, &injected)

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, false, err
	}
	return out, injected, nil
}

// walk recursively traverses the JSON structure. `injectHere` controls
// whether top-level injection should happen at the current node (used to
// suppress injection on sibling array objects).
func walk(node interface{}, fields []string, value interface{}, injectHere bool, injected *bool) interface{} {
	switch v := node.(type) {
	case map[string]interface{}:
		// Recurse into children first so newly-added fields are not walked.
		for k, child := range v {
			v[k] = walk(child, fields, value, true, injected)
		}
		if injectHere {
			for _, f := range fields {
				if _, exists := v[f]; exists {
					continue
				}
				v[f] = cloneValue(value)
				*injected = true
			}
		}
		return v
	case []interface{}:
		firstObjectSeen := false
		for i, elem := range v {
			switch e := elem.(type) {
			case map[string]interface{}:
				inject := !firstObjectSeen
				v[i] = walk(e, fields, value, inject, injected)
				firstObjectSeen = true
			default:
				v[i] = walk(elem, fields, value, true, injected)
			}
		}
		return v
	default:
		return v
	}
}

// cloneValue returns a deep copy of v so mutations to one injected field
// don't bleed into another.
func cloneValue(v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

// InjectForm appends each field=valueString to a urlencoded body, skipping
// any field that already exists. The boolean return signals whether at least
// one field was inserted.
func InjectForm(body []byte, fields []string, value interface{}) ([]byte, bool, error) {
	if len(fields) == 0 {
		return body, false, nil
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, false, fmt.Errorf("parse form body: %w", err)
	}

	var rendered string
	switch tv := value.(type) {
	case string:
		rendered = tv
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return nil, false, err
		}
		rendered = string(b)
	}

	injected := false
	for _, f := range fields {
		if values.Has(f) {
			continue
		}
		values.Set(f, rendered)
		injected = true
	}
	return []byte(values.Encode()), injected, nil
}
