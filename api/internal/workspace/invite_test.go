package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/pgtest"
)

func newTestStore(t *testing.T) *PostgresStore {
	return NewStore(pgtest.NewPool(t))
}

func addUser(t *testing.T, s *PostgresStore, email string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := s.pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash, tenant_id, role) VALUES ($1, $2, 'x', 't', 'admin')`,
		id, email); err != nil {
		t.Fatal(err)
	}
	return id
}

func newInvite(workspaceID, email, by string, expires time.Time) *model.WorkspaceInvite {
	return &model.WorkspaceInvite{ID: uuid.NewString(), WorkspaceID: workspaceID, Email: email, Role: RoleMember, InvitedBy: by, ExpiresAt: expires}
}

func TestInviteLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := addUser(t, s, "owner@example.com")
	invitee := addUser(t, s, "bob@example.com")
	other := addUser(t, s, "eve@example.com")
	if err := s.Create(ctx, model.Workspace{ID: "ws-1", UserID: owner, Name: "Prod", Slug: "prod", Env: "production", Role: RoleOwner, Members: 1}); err != nil {
		t.Fatal(err)
	}
	week := time.Now().Add(InviteTTL)

	if err := s.CreateInvite(ctx, newInvite("ws-1", "owner@example.com", owner, week), "h-owner", nil); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("invite owner: err = %v, want ErrAlreadyMember", err)
	}

	first := newInvite("ws-1", "bob@example.com", owner, week)
	if err := s.CreateInvite(ctx, first, "h1", nil); err != nil {
		t.Fatal(err)
	}
	if first.CreatedAt.IsZero() {
		t.Error("CreatedAt not filled")
	}
	// Re-inviting replaces the open invite; the old link stops working.
	second := newInvite("ws-1", "bob@example.com", owner, week)
	if err := s.CreateInvite(ctx, second, "h2", nil); err != nil {
		t.Fatalf("re-invite: %v", err)
	}
	if _, err := s.PreviewInvite(ctx, "h1"); !errors.Is(err, ErrInviteClosed) {
		t.Errorf("old link preview: err = %v, want ErrInviteClosed", err)
	}
	invites, err := s.ListInvites(ctx, "ws-1")
	if err != nil || len(invites) != 1 || invites[0].ID != second.ID {
		t.Fatalf("list = %+v, %v; want only the second invite", invites, err)
	}

	p, err := s.PreviewInvite(ctx, "h2")
	if err != nil {
		t.Fatal(err)
	}
	if p.WorkspaceName != "Prod" || p.InvitedByEmail != "owner@example.com" || p.Email != "bob@example.com" {
		t.Errorf("preview = %+v", p)
	}
	if _, err := s.PreviewInvite(ctx, "nope"); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("unknown preview: err = %v, want ErrInviteNotFound", err)
	}

	if _, err := s.AcceptInvite(ctx, "h2", other, nil); !errors.Is(err, ErrInviteEmailMismatch) {
		t.Fatalf("wrong account: err = %v, want ErrInviteEmailMismatch", err)
	}
	if _, err := s.AcceptInvite(ctx, "h2", uuid.NewString(), nil); !errors.Is(err, ErrInviteEmailMismatch) {
		t.Fatalf("unknown account: err = %v, want ErrInviteEmailMismatch", err)
	}
	full := errors.New("workspace full")
	var checked string
	if _, err := s.AcceptInvite(ctx, "h2", invitee, func(_ context.Context, ws string) error { checked = ws; return full }); !errors.Is(err, full) || checked != "ws-1" {
		t.Fatalf("denied accept: err = %v, checked %q", err, checked)
	}
	if ids, _ := s.AllowedIDs(ctx, invitee); len(ids) != 0 {
		t.Fatalf("denied accept still joined: %v", ids)
	}
	ws, err := s.AcceptInvite(ctx, "h2", invitee, func(context.Context, string) error { return nil })
	if err != nil || ws != "ws-1" {
		t.Fatalf("accept = %q, %v", ws, err)
	}
	if _, err := s.AcceptInvite(ctx, "h2", invitee, nil); !errors.Is(err, ErrInviteClosed) {
		t.Errorf("reuse: err = %v, want ErrInviteClosed", err)
	}

	ids, err := s.AllowedIDs(ctx, invitee)
	if err != nil || len(ids) != 1 || ids[0] != "ws-1" {
		t.Errorf("invitee access = %v, %v", ids, err)
	}
	members, err := s.ListMembers(ctx, "ws-1")
	if err != nil || len(members) != 2 {
		t.Fatalf("members = %+v, %v", members, err)
	}
	if members[0].Email != "owner@example.com" || members[0].Role != RoleOwner || members[1].Email != "bob@example.com" || members[1].Role != RoleMember {
		t.Errorf("members = %+v", members)
	}
	if invites, _ := s.ListInvites(ctx, "ws-1"); len(invites) != 0 {
		t.Errorf("accepted invite still listed: %+v", invites)
	}
	if err := s.CreateInvite(ctx, newInvite("ws-1", "bob@example.com", owner, week), "h3", nil); !errors.Is(err, ErrAlreadyMember) {
		t.Errorf("invite member: err = %v, want ErrAlreadyMember", err)
	}
}

func TestInviteRevokeExpireAndCascade(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := addUser(t, s, "owner@example.com")
	bob := addUser(t, s, "bob@example.com")
	if err := s.Create(ctx, model.Workspace{ID: "ws-1", UserID: owner, Name: "Prod", Slug: "prod", Env: "production", Role: RoleOwner, Members: 1}); err != nil {
		t.Fatal(err)
	}

	inv := newInvite("ws-1", "bob@example.com", owner, time.Now().Add(InviteTTL))
	if err := s.CreateInvite(ctx, inv, "h1", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeInvite(ctx, "ws-other", inv.ID); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("revoke from another workspace: err = %v, want ErrInviteNotFound", err)
	}
	if err := s.RevokeInvite(ctx, "ws-1", "not-a-uuid"); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("revoke malformed id: err = %v, want ErrInviteNotFound", err)
	}
	if err := s.RevokeInvite(ctx, "ws-1", inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeInvite(ctx, "ws-1", inv.ID); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("second revoke: err = %v, want ErrInviteNotFound", err)
	}
	if _, err := s.AcceptInvite(ctx, "h1", bob, nil); !errors.Is(err, ErrInviteClosed) {
		t.Errorf("accept revoked: err = %v, want ErrInviteClosed", err)
	}

	expired := newInvite("ws-1", "bob@example.com", owner, time.Now().Add(-time.Minute))
	if err := s.CreateInvite(ctx, expired, "h2", nil); err != nil {
		t.Fatal(err)
	}
	if invites, _ := s.ListInvites(ctx, "ws-1"); len(invites) != 0 {
		t.Errorf("expired invite listed: %+v", invites)
	}
	if _, err := s.AcceptInvite(ctx, "h2", bob, nil); !errors.Is(err, ErrInviteClosed) {
		t.Errorf("accept expired: err = %v, want ErrInviteClosed", err)
	}

	open := newInvite("ws-1", "carol@example.com", owner, time.Now().Add(InviteTTL))
	if err := s.CreateInvite(ctx, open, "h3", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "ws-1", owner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewInvite(ctx, "h3"); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("invite outlived its workspace: err = %v", err)
	}
}

func TestCreateInviteSeatCheck(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := addUser(t, s, "owner@example.com")
	if err := s.Create(ctx, model.Workspace{ID: "ws-1", UserID: owner, Name: "Prod", Slug: "prod", Env: "production", Role: RoleOwner, Members: 1}); err != nil {
		t.Fatal(err)
	}
	week := time.Now().Add(InviteTTL)
	full := errors.New("full")
	refuse := func(context.Context) error { return full }

	if err := s.CreateInvite(ctx, newInvite("ws-1", "bob@example.com", owner, week), "h1", nil); err != nil {
		t.Fatal(err)
	}
	// A new link for a pending invite takes no new seat, so allow is skipped.
	if err := s.CreateInvite(ctx, newInvite("ws-1", "bob@example.com", owner, week), "h2", refuse); err != nil {
		t.Fatalf("new link: %v", err)
	}
	// A new address is checked, and a refusal stores nothing.
	if err := s.CreateInvite(ctx, newInvite("ws-1", "carol@example.com", owner, week), "h3", refuse); !errors.Is(err, full) {
		t.Fatalf("new invite: err = %v, want full", err)
	}
	if invites, _ := s.ListInvites(ctx, "ws-1"); len(invites) != 1 || invites[0].Email != "bob@example.com" {
		t.Fatalf("invites = %+v", invites)
	}
	// An existing member is reported before any seat check.
	if err := s.CreateInvite(ctx, newInvite("ws-1", "owner@example.com", owner, week), "h4", refuse); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("member: err = %v, want ErrAlreadyMember", err)
	}
}

func TestGrantMember(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := addUser(t, s, "owner@example.com")
	bob := addUser(t, s, "bob@example.com")
	carol := addUser(t, s, "carol@example.com")
	if err := s.Create(ctx, model.Workspace{ID: "ws-1", UserID: owner, Name: "Prod", Slug: "prod", Env: "production", Role: RoleOwner, Members: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInvite(ctx, newInvite("ws-1", "bob@example.com", owner, time.Now().Add(InviteTTL)), "h1", nil); err != nil {
		t.Fatal(err)
	}
	var seen []bool
	record := func(_ context.Context, invited bool) error { seen = append(seen, invited); return nil }

	// An invited user's seat is already held; the add closes their invite.
	if err := s.GrantMember(ctx, "ws-1", bob, record); err != nil {
		t.Fatal(err)
	}
	if invites, _ := s.ListInvites(ctx, "ws-1"); len(invites) != 0 {
		t.Fatalf("bob's invite still open: %+v", invites)
	}
	// Re-adding a member takes no seat and skips the check.
	if err := s.GrantMember(ctx, "ws-1", bob, func(context.Context, bool) error { return errors.New("checked") }); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	// A refused add stores nothing.
	full := errors.New("full")
	if err := s.GrantMember(ctx, "ws-1", carol, func(context.Context, bool) error { return full }); !errors.Is(err, full) {
		t.Fatalf("refused add: err = %v", err)
	}
	if err := s.GrantMember(ctx, "ws-1", carol, record); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !seen[0] || seen[1] {
		t.Fatalf("allow saw invited = %v, want [true false]", seen)
	}
	if members, _ := s.ListMembers(ctx, "ws-1"); len(members) != 3 {
		t.Fatalf("members = %d, want 3", len(members))
	}
}

// TestConcurrentAcceptsRespectSeats races two accepts of different invites for
// the last seat. allow counts members on its own connection, as a real
// entitlement provider does, so only the workspace lock keeps both from passing.
func TestConcurrentAcceptsRespectSeats(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := addUser(t, s, "owner@example.com")
	bob := addUser(t, s, "bob@example.com")
	carol := addUser(t, s, "carol@example.com")
	if err := s.Create(ctx, model.Workspace{ID: "ws-1", UserID: owner, Name: "Prod", Slug: "prod", Env: "production", Role: RoleOwner, Members: 1}); err != nil {
		t.Fatal(err)
	}
	week := time.Now().Add(InviteTTL)
	for email, hash := range map[string]string{"bob@example.com": "hb", "carol@example.com": "hc"} {
		if err := s.CreateInvite(ctx, newInvite("ws-1", email, owner, week), hash, nil); err != nil {
			t.Fatal(err)
		}
	}
	full := errors.New("full")
	const limit = 2 // owner + one more
	allow := func(ctx context.Context, ws string) error {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM workspace_members WHERE workspace_id = $1`, ws).Scan(&n); err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond) // widen the window between check and insert
		if n >= limit {
			return full
		}
		return nil
	}
	errs := make(chan error, 2)
	for _, a := range []struct{ hash, user string }{{"hb", bob}, {"hc", carol}} {
		go func() { _, err := s.AcceptInvite(ctx, a.hash, a.user, allow); errs <- err }()
	}
	var joined, refused int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			joined++
		case errors.Is(err, full):
			refused++
		default:
			t.Fatal(err)
		}
	}
	if joined != 1 || refused != 1 {
		t.Fatalf("joined %d, refused %d; want 1 and 1", joined, refused)
	}
	if members, _ := s.ListMembers(ctx, "ws-1"); len(members) != limit {
		t.Fatalf("members = %d, want %d", len(members), limit)
	}
}
