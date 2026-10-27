package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"

	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/oauth"
)

const maxOAuthBody = 16 << 10

// OAuthHandler serves the OAuth endpoints MCP clients discover from the MCP
// endpoint's 401. The authorize page itself is the dashboard's /oauth/authorize,
// which calls AuthorizeInfo and Authorize with the user's session.
type OAuthHandler struct {
	svc    *oauth.Service
	issuer string
}

func NewOAuthHandler(svc *oauth.Service, issuer string) *OAuthHandler {
	return &OAuthHandler{svc: svc, issuer: issuer}
}

// ResourceMetadataURL is where the MCP endpoint's 401 points clients (RFC 9728).
func (h *OAuthHandler) ResourceMetadataURL() string {
	return h.issuer + "/.well-known/oauth-protected-resource/v1/mcp"
}

// ProtectedResource handles GET /.well-known/oauth-protected-resource[/v1/mcp].
func (h *OAuthHandler) ProtectedResource(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"resource":                 h.svc.Resource(),
		"authorization_servers":    []string{h.issuer},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Tracium",
	})
}

// AuthorizationServer handles GET /.well-known/oauth-authorization-server (RFC 8414).
func (h *OAuthHandler) AuthorizationServer(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         h.issuer,
		"authorization_endpoint":                         h.issuer + "/oauth/authorize",
		"token_endpoint":                                 h.issuer + "/v1/oauth/token",
		"registration_endpoint":                          h.issuer + "/v1/oauth/register",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// Register handles POST /v1/oauth/register (RFC 7591). Clients are public: no
// secret is issued, whatever auth method they ask for.
func (h *OAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOAuthBody)
	var body struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body must be a JSON client metadata document")
		return
	}
	client, err := h.svc.Register(r.Context(), body.ClientName, body.RedirectURIs)
	if errors.Is(err, oauth.ErrInvalidRedirect) {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must be 1 to 10 https, loopback http or app-scheme URIs without a fragment")
		return
	}
	if err != nil {
		log.Printf("oauth: register client: %v", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "could not register client")
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
	})
}

// AuthorizeInfo handles GET /v1/oauth/authorize for the consent page: it checks
// the client and redirect before anything is shown or redirected to.
func (h *OAuthHandler) AuthorizeInfo(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	client, ok := h.client(w, r, q.Get("client_id"), q.Get("redirect_uri"))
	if !ok {
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"client_name": client.Name, "redirect_uri": q.Get("redirect_uri")})
}

// Authorize handles POST /v1/oauth/authorize: the signed-in user approves or
// denies the client, and the dashboard follows redirect_to back to it.
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOAuthBody)
	var body struct {
		ClientID            string `json:"client_id"`
		RedirectURI         string `json:"redirect_uri"`
		ResponseType        string `json:"response_type"`
		State               string `json:"state"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
		Resource            string `json:"resource"`
		Approve             bool   `json:"approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondDecodeError(w, err)
		return
	}
	if _, ok := h.client(w, r, body.ClientID, body.RedirectURI); !ok {
		return
	}
	params := url.Values{"iss": {h.issuer}}
	if body.State != "" {
		params.Set("state", body.State)
	}
	if !body.Approve {
		params.Set("error", "access_denied")
		respondJSON(w, http.StatusOK, map[string]string{"redirect_to": withQuery(body.RedirectURI, params)})
		return
	}
	principal, _ := middleware.PrincipalFromContext(r.Context())
	code, err := h.svc.Authorize(r.Context(), principal.UserID, oauth.AuthorizeRequest{
		ClientID: body.ClientID, RedirectURI: body.RedirectURI, ResponseType: body.ResponseType,
		CodeChallenge: body.CodeChallenge, CodeChallengeMethod: body.CodeChallengeMethod, Resource: body.Resource,
	})
	switch {
	case errors.Is(err, oauth.ErrInvalidRequest):
		params.Set("error", "invalid_request")
		params.Set("error_description", "response_type must be code, with an S256 PKCE code_challenge, for this server's resource")
	case err != nil:
		log.Printf("oauth: authorize: %v", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not authorize the client")
		return
	default:
		params.Set("code", code)
	}
	respondJSON(w, http.StatusOK, map[string]string{"redirect_to": withQuery(body.RedirectURI, params)})
}

// Token handles POST /v1/oauth/token. The access token is a read key; it does
// not expire and is revoked from the API keys page.
func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, maxOAuthBody)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "request body must be form-encoded")
		return
	}
	if gt := r.PostForm.Get("grant_type"); gt != "authorization_code" {
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	created, err := h.svc.Exchange(r.Context(), oauth.TokenRequest{
		Code:         r.PostForm.Get("code"),
		ClientID:     r.PostForm.Get("client_id"),
		RedirectURI:  r.PostForm.Get("redirect_uri"),
		CodeVerifier: r.PostForm.Get("code_verifier"),
		Resource:     r.PostForm.Get("resource"),
	})
	if errors.Is(err, oauth.ErrInvalidGrant) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the code is invalid, expired or already used, or does not match this client")
		return
	}
	if err != nil {
		log.Printf("oauth: token: %v", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "could not issue a token")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"access_token": created.Token, "token_type": "Bearer"})
}

func (h *OAuthHandler) client(w http.ResponseWriter, r *http.Request, clientID, redirectURI string) (*oauth.Client, bool) {
	client, err := h.svc.Client(r.Context(), clientID, redirectURI)
	switch {
	case errors.Is(err, oauth.ErrInvalidClient):
		respondError(w, http.StatusBadRequest, "INVALID_CLIENT", "this app is not registered with Tracium")
	case errors.Is(err, oauth.ErrInvalidRedirect):
		respondError(w, http.StatusBadRequest, "INVALID_REDIRECT_URI", "this app did not register that redirect address")
	case err != nil:
		log.Printf("oauth: find client: %v", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not look up the client")
	default:
		return client, true
	}
	return nil, false
}

func withQuery(raw string, params url.Values) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	respondJSON(w, status, map[string]string{"error": code, "error_description": description})
}
