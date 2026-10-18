import type { AuthAppearance } from '../../../extensions';
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AuthShell, AuthButton } from '../../auth/components';
import { formatDate } from '../../../common';
import { requestErrorMessage } from '../../auth/api';
import { apiBaseUrl, APIError, WorkspacesAPI } from '../../../common/api';
import { previewInvite, type InvitePreview } from '../api';
import styles from '../../auth/pages/LoginPage.module.css';

interface InvitePageProps {
  token: string;
  appearance?: AuthAppearance;
  session: { token: string; email: string } | null;
  onAccepted?: (workspaceId: string) => void;
  onDismiss?: () => void;
  onSignOut?: () => void;
}

type Load =
  | { state: 'loading' }
  | { state: 'ready'; invite: InvitePreview }
  | { state: 'error'; message: string };

const INVALID = 'This invite link isn’t valid. Ask the workspace owner for a new one.';
const EXPIRED = 'This invite has expired or was already used. Ask the workspace owner for a new one.';

function loadError(err: unknown): string {
  return requestErrorMessage(
    err,
    { 404: INVALID, 410: EXPIRED, 429: 'Too many attempts. Wait a minute and try again.' },
    'We couldn’t load this invite. Try again in a moment.',
  );
}

export default function InvitePage({ token, appearance, session, onAccepted, onDismiss, onSignOut }: InvitePageProps) {
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [accepting, setAccepting] = useState(false);
  const [acceptError, setAcceptError] = useState<string | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    let cancelled = false;
    setLoad({ state: 'loading' });
    previewInvite(token).then(
      invite => { if (!cancelled) setLoad({ state: 'ready', invite }); },
      err => { if (!cancelled) setLoad({ state: 'error', message: loadError(err) }); },
    );
    return () => { cancelled = true; };
  }, [token]);

  const accept = async () => {
    if (!session || accepting) return;
    setAccepting(true);
    setAcceptError(null);
    try {
      const api = new WorkspacesAPI({ baseUrl: apiBaseUrl(), apiKey: session.token });
      const { workspace_id } = await api.acceptInvite(token);
      navigate('/', { replace: true });
      onAccepted?.(workspace_id);
    } catch (err) {
      const code = err instanceof APIError ? err.code : undefined;
      if (code === 'MEMBER_LIMIT_REACHED') {
        setAcceptError('This workspace has reached its member limit. Ask the workspace owner to make room for you.');
      } else if (code === 'FEATURE_UNAVAILABLE') {
        setAcceptError('This workspace can’t add members right now. Ask the workspace owner for help.');
      } else {
        setAcceptError(requestErrorMessage(
          err,
          { 403: 'This invite was sent to a different email address. Sign in with that address to accept it.', 404: INVALID, 410: EXPIRED },
          'We couldn’t accept this invite. Try again in a moment.',
        ));
      }
    } finally {
      setAccepting(false);
    }
  };

  const dismiss = onDismiss && (
    <AuthButton
      variant="secondary"
      onClick={() => {
        navigate(session ? '/' : '/login', { replace: true });
        onDismiss();
      }}
    >
      {session ? 'Go to dashboard' : 'Dismiss'}
    </AuthButton>
  );

  if (load.state === 'loading') {
    return (
      <AuthShell appearance={appearance} title="Workspace invite" subtitle="Loading invite…">
        <div role="status" className={styles.noticeMuted}>Checking your invite link…</div>
      </AuthShell>
    );
  }

  if (load.state === 'error') {
    return (
      <AuthShell appearance={appearance} title="Workspace invite" subtitle="This invite can’t be used.">
        <div className={styles.form}>
          <div role="alert" className={styles.errorBanner}>{load.message}</div>
          {dismiss}
        </div>
      </AuthShell>
    );
  }

  const { invite } = load;
  const invitedBy = invite.invited_by_email || 'A workspace owner';
  const subtitle = `${invitedBy} invited ${invite.email} to join this workspace.`;
  const wrongAccount = session && session.email.toLowerCase() !== invite.email.toLowerCase();

  return (
    <AuthShell appearance={appearance} title={`Join ${invite.workspace_name}`} subtitle={subtitle}>
      <div className={styles.form}>
        {!session && (
          <>
            <div className={styles.notice}>
              <p>Sign in or create an account with <strong>{invite.email}</strong> to accept. You’ll come back here afterwards.</p>
              <p className={styles.noticeMuted}>This invite expires on {formatDate(invite.expires_at)}.</p>
            </div>
            <AuthButton href="/login">Sign in</AuthButton>
            <AuthButton href="/signup" variant="secondary">Create account</AuthButton>
          </>
        )}

        {session && wrongAccount && (
          <>
            <div role="alert" className={styles.errorBanner}>
              You’re signed in as <strong>{session.email}</strong>, but this invite is for <strong>{invite.email}</strong>. Sign out and sign in with that address to accept.
            </div>
            {onSignOut && (
              <AuthButton
                onClick={() => {
                  onSignOut();
                  navigate(`/invite/${encodeURIComponent(token)}`, { replace: true });
                }}
              >
                Sign out
              </AuthButton>
            )}
            {dismiss}
          </>
        )}

        {session && !wrongAccount && (
          <>
            <div className={styles.notice}>
              <p>You’ll join <strong>{invite.workspace_name}</strong> as a member and see its traces, usage and API keys.</p>
              <p className={styles.noticeMuted}>This invite expires on {formatDate(invite.expires_at)}.</p>
            </div>
            {acceptError && <div role="alert" className={styles.errorBanner}>{acceptError}</div>}
            <button type="button" className={styles.submit} disabled={accepting} onClick={accept}>
              {accepting ? 'Joining…' : 'Accept invite'}
            </button>
            {dismiss}
          </>
        )}
      </div>
    </AuthShell>
  );
}
