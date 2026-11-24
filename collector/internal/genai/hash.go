package genai

import (
	"hash/fnv"
	"strings"
	"unicode/utf8"
)

const (
	attrOpenInferenceIn = "input.value"
	minHashedChars      = 40
)

// ContentHashes fingerprints an LLM call's prompt and completion so identical
// answers can be counted without storing content. Short texts hash to 0.
func ContentHashes(attrs map[string]string) (input, output uint64) {
	in := inputText(attrs)
	if in == "" {
		in = attrs[attrOpenInferenceIn]
	}
	return textHash(in), textHash(llmOutput(attrs))
}

func textHash(s string) uint64 {
	s = strings.ToLower(strings.TrimSpace(s))
	if utf8.RuneCountInString(s) < minHashedChars {
		return 0
	}
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
