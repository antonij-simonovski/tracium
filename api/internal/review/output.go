package review

import "github.com/tracium/api/internal/model"

// RunOutput is the root span's output, else the output of the span that started last, as the dashboard does.
func RunOutput(spans []model.Span) string {
	var last *model.Span
	for i := range spans {
		s := &spans[i]
		if s.Output == "" {
			continue
		}
		if s.ParentSpanID == "" {
			return s.Output
		}
		if last == nil || s.StartTimeMs >= last.StartTimeMs {
			last = s
		}
	}
	if last == nil {
		return ""
	}
	return last.Output
}
