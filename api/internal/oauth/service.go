// Package oauth lets MCP clients connect with the OAuth authorization code flow
// and PKCE instead of a pasted read key. The access token it issues is a read key.
package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/tracium/api/internal/apikey"
	tokens "github.com/tracium/api/internal/token"
)

const (
	codePrefix      = "tco_"
	codeTTL         = time.Minute
	maxRedirectURIs = 10
	maxClientName   = 100
	maxURILength    = 2000
	defaultName     = "MCP client"
)

var (
	ErrInvalidRedirect = errors.New("invalid redirect uri")
	ErrInvalidClient   = errors.New("unknown client")
	ErrInvalidRequest  = errors.New("invalid request")
	ErrInvalidGrant    = errors.New("invalid grant")
)

// PKCE code challenges and verifiers are 43 to 128 unreserved characters (RFC 7636).
var pkceValue = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// Schemes a browser must never be sent to; any other non-http scheme is a
// native app's private-use redirect (RFC 8252).
var unsafeSchemes = map[string]bool{"javascript": true, "data": true, "file": true, "vbscript": true, "blob": true, "about": true, "ftp": true, "ws": true, "wss": true}

type KeyIssuer interface {
	CreateRead(ctx context.Context, userID, name string) (apikey.Created, error)
}

type Service struct {
	store    Store
	keys     KeyIssuer
	resource string
}

// NewService serves the protected resource at resource, the MCP endpoint's URL.
func NewService(store Store, keys KeyIssuer, resource string) *Service {
	return &Service{store: store, keys: keys, resource: resource}
}

func (s *Service) Resource() string { return s.resource }

func (s *Service) Register(ctx context.Context, name string, redirectURIs []string) (Client, error) {
	if len(redirectURIs) == 0 || len(redirectURIs) > maxRedirectURIs {
		return Client{}, ErrInvalidRedirect
	}
	for _, u := range redirectURIs {
		if !validRedirect(u) {
			return Client{}, ErrInvalidRedirect
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultName
	}
	if utf8.RuneCountInString(name) > maxClientName {
		name = string([]rune(name)[:maxClientName])
	}
	c := Client{ID: uuid.NewString(), Name: name, RedirectURIs: redirectURIs}
	createdAt, err := s.store.InsertClient(ctx, c)
	c.CreatedAt = createdAt
	return c, err
}

// Client returns the client only when redirectURI is one it registered.
func (s *Service) Client(ctx context.Context, clientID, redirectURI string) (*Client, error) {
	c, err := s.store.Client(ctx, clientID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidClient
	}
	if err != nil {
		return nil, err
	}
	for _, u := range c.RedirectURIs {
		if u == redirectURI {
			return c, nil
		}
	}
	return nil, ErrInvalidRedirect
}

type AuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
}

// Authorize records userID's approval and returns a one-time code. The caller
// has already validated the client and redirect with Client.
func (s *Service) Authorize(ctx context.Context, userID string, req AuthorizeRequest) (string, error) {
	if req.ResponseType != "code" || req.CodeChallengeMethod != "S256" || !pkceValue.MatchString(req.CodeChallenge) || !s.resourceOK(req.Resource) {
		return "", ErrInvalidRequest
	}
	code, err := tokens.New(codePrefix)
	if err != nil {
		return "", err
	}
	err = s.store.InsertCode(ctx, tokens.Hash(code), Code{
		ClientID: req.ClientID, UserID: userID, RedirectURI: req.RedirectURI,
		Challenge: req.CodeChallenge,
	}, codeTTL)
	return code, err
}

type TokenRequest struct {
	Code         string
	ClientID     string
	RedirectURI  string
	CodeVerifier string
	Resource     string
}

// Exchange trades a code for a new read key named after the client.
func (s *Service) Exchange(ctx context.Context, req TokenRequest) (apikey.Created, error) {
	if !tokens.Valid(codePrefix, req.Code) || !pkceValue.MatchString(req.CodeVerifier) || !s.resourceOK(req.Resource) {
		return apikey.Created{}, ErrInvalidGrant
	}
	code, err := s.store.ConsumeCode(ctx, tokens.Hash(req.Code))
	if errors.Is(err, ErrNotFound) {
		return apikey.Created{}, ErrInvalidGrant
	}
	if err != nil {
		return apikey.Created{}, err
	}
	sum := sha256.Sum256([]byte(req.CodeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if code.ClientID != req.ClientID || code.RedirectURI != req.RedirectURI ||
		subtle.ConstantTimeCompare([]byte(challenge), []byte(code.Challenge)) != 1 {
		return apikey.Created{}, ErrInvalidGrant
	}
	client, err := s.store.Client(ctx, code.ClientID)
	if errors.Is(err, ErrNotFound) {
		return apikey.Created{}, ErrInvalidGrant
	}
	if err != nil {
		return apikey.Created{}, err
	}
	return s.keys.CreateRead(ctx, code.UserID, client.Name)
}

// resourceOK accepts an absent resource indicator or this server's own (RFC 8707).
func (s *Service) resourceOK(resource string) bool {
	return resource == "" || strings.TrimRight(resource, "/") == s.resource
}

func validRedirect(raw string) bool {
	if len(raw) > maxURILength {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil {
		return false
	}
	switch {
	case u.Scheme == "https":
		return u.Host != ""
	case u.Scheme == "http":
		host := u.Hostname()
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	default:
		return u.Scheme != "" && !unsafeSchemes[u.Scheme]
	}
}
