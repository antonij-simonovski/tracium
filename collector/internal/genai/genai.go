// Package genai flattens GenAI content attributes from incoming OTLP spans into
// the canonical Span content fields. It understands two conventions so Tracium
// works out of the box with common instrumentation:
//
//   - Current OTel GenAI semconv: gen_ai.input.messages / gen_ai.output.messages
//     (JSON arrays), gen_ai.system_instructions and gen_ai.tool.definitions.
//   - OpenLLMetry (Traceloop) legacy indexed form: gen_ai.prompt.{i}.content /
//     gen_ai.completion.{i}.content, plus llm.request.functions.{i}.* for tools.
//   - Traceloop decorator (workflow/task/agent/tool) spans: traceloop.entity.input /
//     traceloop.entity.output, carrying the wrapped function's args and result as
//     JSON. These are the only content on structural spans (which have no gen_ai.*),
//     so each step's own input/output is visible — not just the LLM child's.
//
// It is pure and framework-free (operates on a plain attribute map) so the
// processor and exporter can share it and it stays unit-testable.
package genai

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	attrInputMessages      = "gen_ai.input.messages"
	attrOutputMessages     = "gen_ai.output.messages"
	attrSystemInstructions = "gen_ai.system_instructions"
	attrToolDefinitions    = "gen_ai.tool.definitions" // semconv tool definitions (JSON array)

	prefixPrompt     = "gen_ai.prompt."         // OpenLLMetry indexed input
	prefixCompletion = "gen_ai.completion."     // OpenLLMetry indexed output
	prefixFunctions  = "llm.request.functions." // OpenLLMetry tool definitions

	// Traceloop decorator spans carry the wrapped function's args/result here.
	attrEntityInput  = "traceloop.entity.input"
	attrEntityOutput = "traceloop.entity.output"

	// maxIndex bounds scanning of indexed attributes (gen_ai.prompt.0, .1, ...)
	// and the number of tools kept from a definitions array.
	maxIndex = 256

	// maxToolDefsBytes bounds the client-controlled gen_ai.tool.definitions
	// string we are willing to parse. Beyond it the attribute is ignored.
	maxToolDefsBytes = 1 << 20
)

// Tool is one entry of the available_tools list.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Used        bool   `json:"used"`
}

// IsContentKey reports whether an attribute carries raw prompt/completion
// content, and so must be dropped when content capture is disabled. Tool
// definitions (llm.request.functions.*) are metadata, not content, and are kept.
func IsContentKey(key string) bool {
	switch key {
	case attrInputMessages, attrOutputMessages, attrSystemInstructions,
		attrEntityInput, attrEntityOutput:
		return true
	}
	return strings.HasPrefix(key, prefixPrompt) || strings.HasPrefix(key, prefixCompletion)
}

// Content flattens the prompt/completion attributes into input/output text.
// Empty strings mean "absent": when content capture is disabled upstream the
// content attributes have already been stripped, so both come back empty.
func Content(attrs map[string]string) (input, output string) {
	return inputText(attrs), outputText(attrs)
}

func inputText(attrs map[string]string) string {
	var b strings.Builder
	if sys := attrs[attrSystemInstructions]; sys != "" {
		b.WriteString("system: ")
		b.WriteString(sys)
		b.WriteByte('\n')
	}
	if msgs := attrs[attrInputMessages]; msgs != "" {
		b.WriteString(renderMessages(msgs))
	} else {
		b.WriteString(renderIndexed(attrs, prefixPrompt))
	}
	if text := strings.TrimRight(b.String(), "\n"); text != "" {
		return text
	}
	// Structural Traceloop spans (workflow/task/agent/tool) have no gen_ai.*
	// content; their function args live here as JSON.
	return attrs[attrEntityInput]
}

func outputText(attrs map[string]string) string {
	if msgs := attrs[attrOutputMessages]; msgs != "" {
		return strings.TrimRight(renderMessages(msgs), "\n")
	}
	if text := strings.TrimRight(renderIndexed(attrs, prefixCompletion), "\n"); text != "" {
		return text
	}
	return attrs[attrEntityOutput]
}

// renderIndexed joins OpenLLMetry indexed messages ("role: content" per line)
// until the first absent index.
func renderIndexed(attrs map[string]string, prefix string) string {
	var b strings.Builder
	for i := 0; i < maxIndex; i++ {
		role, hasRole := attrs[prefix+strconv.Itoa(i)+".role"]
		content, hasContent := attrs[prefix+strconv.Itoa(i)+".content"]
		if !hasRole && !hasContent {
			break
		}
		if role != "" {
			b.WriteString(role)
			b.WriteString(": ")
		}
		b.WriteString(content)
		b.WriteByte('\n')
	}
	return b.String()
}

// message is the subset of the OTel GenAI message schema we render. Parts carry
// either "content" (current) or "text"; a flat top-level "content" is accepted
// as a fallback.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Parts   []struct {
		Content string `json:"content"`
		Text    string `json:"text"`
	} `json:"parts"`
}

// renderMessages flattens a gen_ai.{input,output}.messages JSON array. If it
// isn't JSON we recognise, the raw string is returned unchanged.
func renderMessages(jsonStr string) string {
	var msgs []message
	if err := json.Unmarshal([]byte(jsonStr), &msgs); err != nil {
		return jsonStr
	}
	var b strings.Builder
	for _, m := range msgs {
		if m.Role != "" {
			b.WriteString(m.Role)
			b.WriteString(": ")
		}
		if len(m.Parts) > 0 {
			for _, p := range m.Parts {
				if p.Content != "" {
					b.WriteString(p.Content)
				} else {
					b.WriteString(p.Text)
				}
			}
		} else {
			b.WriteString(m.Content)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ToolsJSON builds the JSON available_tools array from the tool definitions
// offered on the span, marking each tool used if it appears in the completion's
// tool calls. Prefers the semconv gen_ai.tool.definitions array and falls back
// to OpenLLMetry's indexed llm.request.functions.{i}.*. Returns "" when no tools
// were offered.
func ToolsJSON(attrs map[string]string) string {
	list := toolDefinitions(attrs[attrToolDefinitions])
	if len(list) == 0 {
		list = indexedToolDefinitions(attrs)
	}
	if len(list) == 0 {
		return ""
	}
	used := usedToolNames(attrs)
	for i := range list {
		list[i].Used = used[list[i].Name]
	}
	out, err := json.Marshal(list)
	if err != nil {
		return ""
	}
	return string(out)
}

// toolDefinitions parses the semconv gen_ai.tool.definitions JSON array. The
// string is client-controlled, so anything oversized, malformed or of an
// unexpected element shape yields no tools rather than garbage. Both the flat
// ({"name":...}) and OpenAI-style nested ({"function":{"name":...}}) shapes are
// accepted.
func toolDefinitions(jsonStr string) []Tool {
	if jsonStr == "" || len(jsonStr) > maxToolDefsBytes {
		return nil
	}
	var defs []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Function    struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"function"`
	}
	if json.Unmarshal([]byte(jsonStr), &defs) != nil {
		return nil
	}
	var list []Tool
	for _, d := range defs {
		if len(list) == maxIndex {
			break
		}
		name, desc := d.Name, d.Description
		if name == "" {
			name, desc = d.Function.Name, d.Function.Description
		}
		if name == "" {
			continue // unnamed entry: nothing usable to record
		}
		list = append(list, Tool{Name: name, Description: desc})
	}
	return list
}

// indexedToolDefinitions reads OpenLLMetry's legacy indexed function
// definitions, up to the first absent index.
func indexedToolDefinitions(attrs map[string]string) []Tool {
	var list []Tool
	for i := 0; i < maxIndex; i++ {
		name, hasName := attrs[prefixFunctions+strconv.Itoa(i)+".name"]
		desc, hasDesc := attrs[prefixFunctions+strconv.Itoa(i)+".description"]
		if !hasName && !hasDesc {
			break
		}
		list = append(list, Tool{Name: name, Description: desc})
	}
	return list
}

// usedToolNames collects the names of tools the model actually called, from both
// the OpenLLMetry indexed completion tool_calls and the OTel output messages.
// Best-effort: tool-call shapes vary across instrumentation versions.
func usedToolNames(attrs map[string]string) map[string]bool {
	used := map[string]bool{}
	for i := 0; i < maxIndex; i++ {
		ci := prefixCompletion + strconv.Itoa(i)
		if !completionIndexPresent(attrs, ci) {
			break
		}
		for j := 0; j < maxIndex; j++ {
			base := ci + ".tool_calls." + strconv.Itoa(j)
			name, ok := attrs[base+".name"]
			if !ok {
				name, ok = attrs[base+".function.name"]
			}
			if !ok {
				break
			}
			if name != "" {
				used[name] = true
			}
		}
	}
	for _, n := range toolNamesFromMessages(attrs[attrOutputMessages]) {
		used[n] = true
	}
	return used
}

func completionIndexPresent(attrs map[string]string, ci string) bool {
	for _, suffix := range []string{".role", ".content", ".finish_reason", ".tool_calls.0.name", ".tool_calls.0.function.name"} {
		if _, ok := attrs[ci+suffix]; ok {
			return true
		}
	}
	return false
}

// toolNamesFromMessages best-effort extracts tool-call names from an OTel
// gen_ai.output.messages array.
func toolNamesFromMessages(jsonStr string) []string {
	if jsonStr == "" {
		return nil
	}
	var msgs []struct {
		Parts []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"parts"`
		ToolCalls []struct {
			Name     string `json:"name"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal([]byte(jsonStr), &msgs) != nil {
		return nil
	}
	var names []string
	for _, m := range msgs {
		for _, p := range m.Parts {
			if strings.Contains(p.Type, "tool") && p.Name != "" {
				names = append(names, p.Name)
			}
		}
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				names = append(names, tc.Name)
			}
			if tc.Function.Name != "" {
				names = append(names, tc.Function.Name)
			}
		}
	}
	return names
}
