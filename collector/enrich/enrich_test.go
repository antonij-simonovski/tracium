package enrich

import (
	"context"
	"errors"
	"strings"
	"testing"

	customerrors "github.com/tracium/collector/internal/errors"
	"github.com/tracium/collector/internal/pricing"
	"github.com/tracium/collector/pkg/spanmodel"
)

func validSpan() *spanmodel.Span {
	return &spanmodel.Span{
		TraceID:      "t1",
		SpanID:       "s1",
		StartTimeMs:  1,
		Model:        "  GPT-4o ",
		InputTokens:  1000,
		OutputTokens: 1000,
	}
}

func TestDefaultChain_HappyPath(t *testing.T) {
	chain := DefaultChain(pricing.NewStaticResolver(pricing.DefaultPrices()), nil, nil)
	span := validSpan()

	if err := chain.Apply(context.Background(), span); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.ModelNormalized != "gpt-4o" {
		t.Errorf("ModelNormalized = %q, want %q", span.ModelNormalized, "gpt-4o")
	}
	// gpt-4o: 1000*0.0000025 + 1000*0.00001 = 0.0125
	if span.CostUSD != 0.0125 {
		t.Errorf("CostUSD = %v, want 0.0125", span.CostUSD)
	}
	if span.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", span.SchemaVersion)
	}
}

func TestValidate_DropsInvalidSpan(t *testing.T) {
	chain := DefaultChain(nil, nil, nil)
	span := validSpan()
	span.TraceID = "" // invalid

	err := chain.Apply(context.Background(), span)
	if got := customerrors.Code(err); got != string(customerrors.ErrMissingTraceID) {
		t.Fatalf("got %v, want a %s drop", err, customerrors.ErrMissingTraceID)
	}
}

func TestValidate_DropsOutOfRangeTokenCounts(t *testing.T) {
	chain := DefaultChain(pricing.NewStaticResolver(pricing.DefaultPrices()), nil, nil)
	span := validSpan()
	span.InputTokens = 1 << 62 // ~$3.4 trillion at gpt-4o rates

	err := chain.Apply(context.Background(), span)
	if got := customerrors.Code(err); got != string(customerrors.ErrTokenCountOutOfRange) {
		t.Fatalf("got %v, want a %s drop", err, customerrors.ErrTokenCountOutOfRange)
	}
}

func TestValidate_DropsEndBeforeStart(t *testing.T) {
	chain := DefaultChain(nil, nil, nil)
	span := validSpan()
	span.StartTimeMs = 1_700_000_060_000
	span.EndTimeMs = 1_700_000_000_000 // 60s of clock skew

	err := chain.Apply(context.Background(), span)
	if got := customerrors.Code(err); got != string(customerrors.ErrInvalidTimestamp) {
		t.Fatalf("got %v, want a %s drop", err, customerrors.ErrInvalidTimestamp)
	}
}

func TestValidate_DropsOversizedModelName(t *testing.T) {
	chain := DefaultChain(nil, nil, nil)
	span := validSpan()
	span.Model = strings.Repeat("x", 100_000)

	err := chain.Apply(context.Background(), span)
	if got := customerrors.Code(err); got != string(customerrors.ErrFieldTooLong) {
		t.Fatalf("got %v, want a %s drop", err, customerrors.ErrFieldTooLong)
	}
}

func TestUser_ControlCharactersAreStripped(t *testing.T) {
	chain := DefaultChain(nil, nil, nil)
	span := validSpan()
	span.UserID = "evil\x00user\nsecond-line"

	if err := chain.Apply(context.Background(), span); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.UserID != "evilusersecond-line" {
		t.Errorf("UserID = %q, want control characters removed", span.UserID)
	}
}

func TestPricing_CachedTokensReduceCost(t *testing.T) {
	chain := DefaultChain(pricing.NewStaticResolver(pricing.DefaultPrices()), nil, nil)
	span := validSpan()
	span.OutputTokens = 0
	span.CacheReadTokens = 800 // 800 of the 1000 input tokens came from cache

	if err := chain.Apply(context.Background(), span); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// gpt-4o: 200 regular input * 0.0000025 + 800 cached * 0.00000125
	want := 200*0.0000025 + 800*0.00000125
	if span.CostUSD != want {
		t.Errorf("CostUSD = %v, want %v", span.CostUSD, want)
	}
}

func TestPricing_UnknownModelKeepsSpanWithZeroCost(t *testing.T) {
	chain := DefaultChain(pricing.NewStaticResolver(pricing.DefaultPrices()), nil, nil)
	span := validSpan()
	span.Model = "some-unlisted-model"

	if err := chain.Apply(context.Background(), span); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0 for unpriced model", span.CostUSD)
	}
}

func TestFilter_DropsDisallowedModel(t *testing.T) {
	chain := DefaultChain(nil, nil, []string{"gpt-4o"})

	span := validSpan()
	span.Model = "claude-3-opus"
	if err := chain.Apply(context.Background(), span); customerrors.Code(err) != string(customerrors.ErrUnknownModel) {
		t.Errorf("disallowed model: got %v, want an unknown_model drop", err)
	}

	allowed := validSpan() // normalizes to gpt-4o
	if err := chain.Apply(context.Background(), allowed); err != nil {
		t.Errorf("allowed model: got %v, want kept", err)
	}
}

type failingUserResolver struct{}

func (failingUserResolver) Resolve(context.Context, string) (string, error) {
	return "", errors.New("postgres down")
}

func TestUser_LookupFailureDropsSpan(t *testing.T) {
	chain := DefaultChain(nil, failingUserResolver{}, nil)
	span := validSpan()
	span.UserID = "api-key-123"

	err := chain.Apply(context.Background(), span)
	if got := customerrors.Code(err); got != string(customerrors.ErrUserLookupFailed) {
		t.Fatalf("got %v, want a user_lookup_failed drop", err)
	}
}

func TestFilter_MatchesAllowedModelCaseInsensitively(t *testing.T) {
	chain := DefaultChain(nil, nil, []string{"GPT-4o"})

	if err := chain.Apply(context.Background(), validSpan()); err != nil {
		t.Errorf("got %v, want kept", err)
	}
}
