// Package review stores human reviews of runs and grades JSON outputs per field.
package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/tracium/api/internal/model"
)

// Grade compares output with corrected field by field when both are JSON
// objects; ok is false otherwise. The corrected side is the truth, so a field
// missing from either side is wrong. Arrays are graded as one field.
func Grade(output, corrected string) (fields []model.ReviewField, ok bool) {
	out, ok1 := parseObject(output)
	want, ok2 := parseObject(corrected)
	if !ok1 || !ok2 {
		return nil, false
	}
	got, exp := map[string]any{}, map[string]any{}
	flatten("", out, got)
	flatten("", want, exp)

	paths := map[string]bool{}
	for p := range got {
		paths[p] = true
	}
	for p := range exp {
		paths[p] = true
	}
	for p := range paths {
		g, inGot := got[p]
		e, inExp := exp[p]
		fields = append(fields, model.ReviewField{
			Path:      p,
			Output:    raw(g, inGot),
			Corrected: raw(e, inExp),
			Correct:   inGot && inExp && equal(g, e),
			Missing:   !inGot && inExp,
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })
	return fields, true
}

// Verdict is correct only when every field is.
func Verdict(fields []model.ReviewField) string {
	for _, f := range fields {
		if !f.Correct {
			return model.VerdictIncorrect
		}
	}
	return model.VerdictCorrect
}

func parseObject(s string) (map[string]any, bool) {
	// The collector stores an LLM span's output as "assistant: <content>".
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "assistant:"))
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "```json"), "```")
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil || v == nil || dec.More() {
		return nil, false
	}
	return v, true
}

// shortcut: a key containing "." can share a path with a nested key ({"a.b": 1}
// and {"a": {"b": 2}}), and then one of them is graded at random; escape keys if
// outputs with dotted keys show up.
func flatten(prefix string, obj map[string]any, into map[string]any) {
	if len(obj) == 0 && prefix != "" {
		into[prefix] = obj
		return
	}
	for k, v := range obj {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			flatten(p, child, into)
			continue
		}
		into[p] = v
	}
}

func raw(v any, present bool) string {
	if !present {
		return ""
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(buf.String())
}

// equal is deep equality where 10 and 10.0 are the same number.
func equal(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		rx, ok1 := new(big.Rat).SetString(x.String())
		ry, ok2 := new(big.Rat).SetString(y.String())
		return ok1 && ok2 && rx.Cmp(ry) == 0
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !equal(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// ErrNotObject is returned by Patch when the output is not a JSON object.
var ErrNotObject = errors.New("output is not a JSON object")

// Patch returns output with each dotted path in corrections set to its value,
// so fields left out keep the output's value and grade as correct.
func Patch(output string, corrections map[string]json.RawMessage) (string, error) {
	obj, ok := parseObject(output)
	if !ok {
		return "", ErrNotObject
	}
	for path, value := range corrections {
		dec := json.NewDecoder(bytes.NewReader(value))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return "", fmt.Errorf("corrections.%s: %w", path, err)
		}
		keys := strings.Split(path, ".")
		parent := obj
		for _, k := range keys[:len(keys)-1] {
			child, exists := parent[k]
			if !exists {
				child = map[string]any{}
				parent[k] = child
			}
			m, isObj := child.(map[string]any)
			if !isObj {
				return "", fmt.Errorf("corrections.%s: %s is not an object", path, k)
			}
			parent = m
		}
		parent[keys[len(keys)-1]] = v
	}
	return raw(obj, true), nil
}
