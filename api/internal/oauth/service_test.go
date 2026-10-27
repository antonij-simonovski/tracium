package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/oauth"
	"github.com/tracium/api/testing/mocks"
)

const resource = "https://tracium.example.com/v1/mcp"

type issuer struct{ user, name string }

func (i *issuer) CreateRead(_ context.Context, userID, name string) (apikey.Created, error) {
	i.user, i.name = userID, name
	return apikey.Created{Token: "trc_key"}, nil
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestRegisterValidatesRedirects(t *testing.T) {
	svc := oauth.NewService(mocks.NewOAuthStore(), &issuer{}, resource)
	ok := []string{
		"https://claude.ai/api/mcp/auth_callback",
		"http://localhost:33418/callback",
		"http://127.0.0.1:8080/cb",
		"http://[::1]/cb",
		"cursor://anysphere.cursor-retrieval/oauth/callback",
	}
	for _, u := range ok {
		if _, err := svc.Register(context.Background(), "c", []string{u}); err != nil {
			t.Errorf("%s rejected: %v", u, err)
		}
	}
	bad := []string{
		"http://evil.example.com/cb",
		"https://claude.ai/cb#frag",
		"https://user:pw@claude.ai/cb",
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:text/html,hi",
		"/relative",
		" https://claude.ai/cb",
		"https:///nohost",
		"https://claude.ai/" + strings.Repeat("a", 2000),
	}
	for _, u := range bad {
		if _, err := svc.Register(context.Background(), "c", []string{u}); !errors.Is(err, oauth.ErrInvalidRedirect) {
			t.Errorf("%s accepted", u)
		}
	}
	if _, err := svc.Register(context.Background(), "c", nil); !errors.Is(err, oauth.ErrInvalidRedirect) {
		t.Error("no redirect uris accepted")
	}
	c, _ := svc.Register(context.Background(), "  ", ok[:1])
	if c.Name != "MCP client" {
		t.Errorf("blank name became %q", c.Name)
	}
}

func TestExchange(t *testing.T) {
	ctx := context.Background()
	keys := &issuer{}
	svc := oauth.NewService(mocks.NewOAuthStore(), keys, resource)
	const redirect = "https://claude.ai/api/mcp/auth_callback"
	client, err := svc.Register(ctx, "Claude", []string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Client(ctx, client.ID, "https://claude.ai/other"); !errors.Is(err, oauth.ErrInvalidRedirect) {
		t.Errorf("unregistered redirect: %v", err)
	}
	if _, err := svc.Client(ctx, "nope", redirect); !errors.Is(err, oauth.ErrInvalidClient) {
		t.Errorf("unknown client: %v", err)
	}

	verifier := strings.Repeat("v", 43)
	req := oauth.AuthorizeRequest{ClientID: client.ID, RedirectURI: redirect, ResponseType: "code", CodeChallenge: challenge(verifier), CodeChallengeMethod: "S256", Resource: resource}
	for name, mutate := range map[string]func(*oauth.AuthorizeRequest){
		"plain method":   func(r *oauth.AuthorizeRequest) { r.CodeChallengeMethod = "plain" },
		"token response": func(r *oauth.AuthorizeRequest) { r.ResponseType = "token" },
		"short":          func(r *oauth.AuthorizeRequest) { r.CodeChallenge = "abc" },
		"other resource": func(r *oauth.AuthorizeRequest) { r.Resource = "https://evil.example.com/mcp" },
	} {
		bad := req
		mutate(&bad)
		if _, err := svc.Authorize(ctx, "user-1", bad); !errors.Is(err, oauth.ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}

	exchange := oauth.TokenRequest{ClientID: client.ID, RedirectURI: redirect, CodeVerifier: verifier, Resource: resource + "/"}
	for name, mutate := range map[string]func(*oauth.TokenRequest){
		"wrong verifier": func(r *oauth.TokenRequest) { r.CodeVerifier = strings.Repeat("w", 43) },
		"wrong client":   func(r *oauth.TokenRequest) { r.ClientID = "other" },
		"wrong redirect": func(r *oauth.TokenRequest) { r.RedirectURI = "https://claude.ai/other" },
	} {
		code, err := svc.Authorize(ctx, "user-1", req)
		if err != nil {
			t.Fatal(err)
		}
		bad := exchange
		bad.Code = code
		mutate(&bad)
		if _, err := svc.Exchange(ctx, bad); !errors.Is(err, oauth.ErrInvalidGrant) {
			t.Errorf("%s: %v", name, err)
		}
		// A failed exchange still spends the code.
		exchange.Code = code
		if _, err := svc.Exchange(ctx, exchange); !errors.Is(err, oauth.ErrInvalidGrant) {
			t.Errorf("%s: code reusable after a failed exchange", name)
		}
	}

	code, err := svc.Authorize(ctx, "user-1", req)
	if err != nil {
		t.Fatal(err)
	}
	exchange.Code = code
	created, err := svc.Exchange(ctx, exchange)
	if err != nil || created.Token != "trc_key" || keys.user != "user-1" || keys.name != "Claude" {
		t.Fatalf("exchange: %v %+v %+v", err, created, keys)
	}
	if _, err := svc.Exchange(ctx, exchange); !errors.Is(err, oauth.ErrInvalidGrant) {
		t.Error("code worked twice")
	}
}
