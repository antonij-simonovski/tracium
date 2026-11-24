package genai

import "testing"

func TestContentHashes(t *testing.T) {
	const answer = "Your order has shipped and will arrive within five business days."
	a := map[string]string{
		"gen_ai.prompt.0.content":     "Where is order 1234? Please tell me when it arrives.",
		"gen_ai.completion.0.content": answer,
	}
	b := map[string]string{
		"gen_ai.prompt.0.content":     "Where is order 9876? Please tell me when it arrives.",
		"gen_ai.completion.0.content": "  " + "YOUR ORDER HAS SHIPPED and will arrive within five business days.\n",
	}
	inA, outA := ContentHashes(a)
	inB, outB := ContentHashes(b)
	if outA == 0 || outA != outB {
		t.Errorf("same answer after trim and case: %x vs %x", outA, outB)
	}
	if inA == 0 || inA == inB {
		t.Errorf("different prompts must hash apart: %x vs %x", inA, inB)
	}

	short := map[string]string{"gen_ai.completion.0.content": "OK, done."}
	if _, out := ContentHashes(short); out != 0 {
		t.Errorf("short output hashed to %x, want 0", out)
	}

	oi := map[string]string{"input.value": "Summarise the attached quarterly report for the board.", "output.value": answer}
	if in, out := ContentHashes(oi); in == 0 || out == 0 {
		t.Errorf("OpenInference fallback not hashed: %x %x", in, out)
	}
}
