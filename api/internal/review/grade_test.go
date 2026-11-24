package review

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/tracium/api/internal/model"
)

func TestGrade(t *testing.T) {
	out := "```json\n" + `{"total": 10, "due": "2026-01-01", "made_up": 1, "customer": {"name": "Ann", "city": "Oslo"}, "items": [1, 2], "meta": {}}` + "\n```"
	fixed := `{"total": 10.0, "due": "2026-02-01", "customer": {"name": "Ann", "city": "Bergen", "zip": "5003"}, "items": [1, 2], "meta": {}}`

	fields, ok := Grade(out, fixed)
	if !ok {
		t.Fatal("not graded")
	}
	want := map[string]bool{
		"total": true, "due": false, "made_up": false, "customer.name": true,
		"customer.city": false, "customer.zip": false, "items": true, "meta": true,
	}
	if len(fields) != len(want) {
		t.Fatalf("fields = %+v", fields)
	}
	for _, f := range fields {
		if c, ok := want[f.Path]; !ok || c != f.Correct {
			t.Errorf("%s correct=%v", f.Path, f.Correct)
		}
	}
	byPath := map[string]model.ReviewField{}
	for _, f := range fields {
		byPath[f.Path] = f
	}
	if f := byPath["made_up"]; f.Output != "1" || f.Corrected != "" {
		t.Errorf("made_up = %+v", f)
	}
	if byPath["made_up"].Missing || !byPath["customer.zip"].Missing || byPath["due"].Missing {
		t.Errorf("missing flags = %+v", byPath)
	}
	if f := byPath["customer.zip"]; f.Output != "" || f.Corrected != `"5003"` {
		t.Errorf("zip = %+v", f)
	}
	if Verdict(fields) != model.VerdictIncorrect {
		t.Error("verdict should be incorrect")
	}

	same, _ := Grade(`{"a": [1, {"b": 2}]}`, `{"a": [1.0, {"b": 2}]}`)
	if Verdict(same) != model.VerdictCorrect {
		t.Errorf("same = %+v", same)
	}

	if llm, ok := Grade(`assistant: {"a": 1}`, `{"a": 2}`); !ok || len(llm) != 1 || llm[0].Output != "1" {
		t.Errorf("LLM span output = %+v, %v", llm, ok)
	}

	for _, pair := range [][2]string{{"plain text", `{"a": 1}`}, {`{"a": 1}`, `[1]`}, {`{"a": 1} {"b": 2}`, `{"a": 1}`}, {"null", `{"a": 1}`}} {
		if _, ok := Grade(pair[0], pair[1]); ok {
			t.Errorf("Grade(%q, %q) graded", pair[0], pair[1])
		}
	}
}

func TestPatch(t *testing.T) {
	out := `assistant: {"vendor": "Acme", "totals": {"total": 10, "tax": 2}}`
	got, err := Patch(out, map[string]json.RawMessage{"totals.total": []byte(`12.50`), "vendor_id": []byte(`"v-9"`)})
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := Grade(out, got)
	wrong := map[string]bool{}
	for _, f := range fields {
		if !f.Correct {
			wrong[f.Path] = true
		}
	}
	if len(fields) != 4 || len(wrong) != 2 || !wrong["totals.total"] || !wrong["vendor_id"] {
		t.Errorf("fields = %+v", fields)
	}
	if _, err := Patch("plain text", nil); !errors.Is(err, ErrNotObject) {
		t.Errorf("text output: err = %v", err)
	}
	if _, err := Patch(out, map[string]json.RawMessage{"vendor.name": []byte(`"x"`)}); err == nil {
		t.Error("patching inside a string should fail")
	}
}
