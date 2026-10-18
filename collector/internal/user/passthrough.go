package user

import "context"

// Passthrough is the default user resolver: it treats the incoming value (an API
// key or a user attribute carried on the span) as the user ID itself, with
// no external lookup.
type Passthrough struct{}

// Resolve returns apiKey unchanged.
func (Passthrough) Resolve(_ context.Context, apiKey string) (string, error) {
	return apiKey, nil
}
