// Package auth verifies the caller and puts the resulting tenant identity in
// the request context. Everything downstream — the store especially — takes
// its scope from here and never from a client-supplied request field.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/expona-ai/lumi-go/server/internal/store"
)

// Identity is the verified caller.
type Identity struct {
	UserID      string
	WorkspaceID string
	// Which client this connection came from: embed-panel, full-page, slack…
	// Carried so per-surface verifier routing can land without a signature
	// change (lumi-neo routes embed-panel to a different verifier than the rest).
	Surface string
}

// Scope projects the identity into the store's tenant scope.
func (i Identity) Scope() store.Scope {
	return store.Scope{UserID: i.UserID, WorkspaceID: i.WorkspaceID}
}

var (
	ErrMissingToken = errors.New("missing bearer token")
	ErrInvalidToken = errors.New("invalid token")
	// ErrUnauthenticated is what the interceptor returns to the client. The
	// specific reason stays server-side.
	ErrUnauthenticated = errors.New("unauthenticated")
)

// Credentials are the raw request-level inputs a verifier may use.
type Credentials struct {
	Token       string
	WorkspaceID string // X-Workspace-Id, honored only by verifiers that trust it
	Surface     string // X-Lumi-Surface
}

// Verifier turns credentials into a verified identity.
type Verifier interface {
	Verify(ctx context.Context, creds Credentials) (Identity, error)
}

// DevBearerVerifier is the INSECURE local-development stub: the bearer token
// IS the user id, and the workspace comes straight off a request header.
// Anyone can claim any identity — never run this in staging or production.
//
// It exists so the chat path is runnable before the real token verifier lands;
// the seam it implements is the one the production verifier will fill.
type DevBearerVerifier struct {
	// Used when the request carries no workspace header, so a bare token still
	// resolves to a usable tenant in local development.
	DefaultWorkspaceID string
}

var _ Verifier = (*DevBearerVerifier)(nil)

func (v *DevBearerVerifier) Verify(_ context.Context, creds Credentials) (Identity, error) {
	token := strings.TrimSpace(creds.Token)
	if token == "" {
		return Identity{}, ErrMissingToken
	}

	// `user:workspace` lets a client pin both halves in one token, which is
	// handy for curl and for the Connect client's single-header setup.
	userID, workspaceID := token, creds.WorkspaceID
	if u, w, ok := strings.Cut(token, ":"); ok && u != "" && w != "" {
		userID, workspaceID = u, w
	}
	if workspaceID == "" {
		workspaceID = v.DefaultWorkspaceID
	}
	if workspaceID == "" {
		return Identity{}, fmt.Errorf("%w: no workspace id (send X-Workspace-Id or a user:workspace token)", ErrInvalidToken)
	}

	return Identity{UserID: userID, WorkspaceID: workspaceID, Surface: creds.Surface}, nil
}

// --- context plumbing -------------------------------------------------------

type contextKey struct{}

// FromContext returns the verified identity. The false case is a programming
// error — the interceptor rejects unauthenticated requests before they reach a
// handler — so handlers should treat it as internal, not as a 401.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(Identity)
	return id, ok
}

func withIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// --- connect interceptor ----------------------------------------------------

const (
	headerAuthorization = "Authorization"
	headerWorkspaceID   = "X-Workspace-Id"
	headerSurface       = "X-Lumi-Surface"
)

// Interceptor authenticates every RPC — unary and streaming alike — and
// attaches the identity to the handler's context.
type Interceptor struct {
	verifier Verifier
}

func NewInterceptor(v Verifier) *Interceptor { return &Interceptor{verifier: v} }

var _ connect.Interceptor = (*Interceptor)(nil)

func credentialsFrom(h interface{ Get(string) string }) Credentials {
	authz := h.Get(headerAuthorization)
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if token == authz {
		// No Bearer prefix — treat the whole header as the token so a plain
		// `Authorization: <token>` still works.
		token = strings.TrimSpace(authz)
	}
	return Credentials{
		Token:       token,
		WorkspaceID: strings.TrimSpace(h.Get(headerWorkspaceID)),
		Surface:     strings.TrimSpace(h.Get(headerSurface)),
	}
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		id, err := i.verifier.Verify(ctx, credentialsFrom(req.Header()))
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated)
		}
		return next(withIdentity(ctx, id), req)
	}
}

func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	// Client-side interception is not used by this service.
	return next
}

func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		id, err := i.verifier.Verify(ctx, credentialsFrom(conn.RequestHeader()))
		if err != nil {
			return connect.NewError(connect.CodeUnauthenticated, ErrUnauthenticated)
		}
		return next(withIdentity(ctx, id), conn)
	}
}
