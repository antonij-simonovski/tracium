import type { AuthAppearance } from '../../../extensions';
import { useEffect, useMemo, useState } from 'react';
import { AuthShell, AuthButton } from '../../auth/components';
import { AuthError, requestErrorMessage } from '../../auth/api';
import { oauthClientInfo, decideOAuth, type OAuthClientInfo } from '../api';
import styles from '../../auth/pages/LoginPage.module.css';

interface ConsentPageProps {
  request: string;
  session: { token: string; email: string };
  appearance?: AuthAppearance;
  onDismiss: () => void;
  onSignOut: () => void;
}

type Load =
  | { state: 'loading' }
  | { state: 'ready'; client: OAuthClientInfo }
  | { state: 'error'; message: string; expired?: boolean };

const INVALID = 'This connection link isn’t valid. Start connecting again from the app.';
const EXPIRED = 'Your session expired. Sign in again to connect this app.';

function destination(redirectURI: string): string {
  try {
    const url = new URL(redirectURI);
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.host : `${url.protocol}//${url.host}`;
  } catch {
    return redirectURI;
  }
}

export default function ConsentPage({ request, session, appearance, onDismiss, onSignOut }: ConsentPageProps) {
  const params = useMemo(() => new URLSearchParams(request), [request]);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [deciding, setDeciding] = useState(false);
  const [decideError, setDecideError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    oauthClientInfo(session.token, params).then(
      client => { if (!cancelled) setLoad({ state: 'ready', client }); },
      err => {
        if (cancelled) return;
        const expired = err instanceof AuthError && err.status === 401;
        setLoad({ state: 'error', expired, message: requestErrorMessage(err, { 400: INVALID, 401: EXPIRED }, 'We couldn’t load this connection. Try again in a moment.') });
      },
    );
    return () => { cancelled = true; };
  }, [params, session.token]);

  const decide = async (approve: boolean) => {
    if (deciding) return;
    setDeciding(true);
    setDecideError(null);
    try {
      const to = await decideOAuth(session.token, params, approve);
      onDismiss();
      window.location.assign(to);
    } catch (err) {
      setDecideError(requestErrorMessage(err, { 400: INVALID, 401: EXPIRED }, 'We couldn’t connect this app. Try again in a moment.'));
      setDeciding(false);
    }
  };

  const leave = (
    <AuthButton variant="secondary" onClick={() => { window.history.replaceState(null, '', '/'); onDismiss(); }}>
      Go to dashboard
    </AuthButton>
  );

  if (load.state === 'loading') {
    return (
      <AuthShell appearance={appearance} title="Connect an app" subtitle="Loading…">
        <div role="status" className={styles.noticeMuted}>Checking the connection request…</div>
      </AuthShell>
    );
  }

  if (load.state === 'error') {
    return (
      <AuthShell appearance={appearance} title="Connect an app" subtitle="This request can’t be used.">
        <div className={styles.form}>
          <div role="alert" className={styles.errorBanner}>{load.message}</div>
          {load.expired ? <AuthButton onClick={onSignOut}>Sign in again</AuthButton> : leave}
        </div>
      </AuthShell>
    );
  }

  const { client } = load;
  return (
    <AuthShell appearance={appearance} title={`Connect ${client.client_name}`} subtitle={`Signed in as ${session.email}`}>
      <div className={styles.form}>
        <div className={styles.notice}>
          <p><strong>{client.client_name}</strong> will be able to read traces, costs and usage in every workspace you can access. It can’t change anything.</p>
          <p className={styles.noticeMuted}>You’ll be sent back to <strong>{destination(client.redirect_uri)}</strong>. Only continue if you started this connection there. You can disconnect it any time by revoking its key under Settings → Account.</p>
        </div>
        {decideError && <div role="alert" className={styles.errorBanner}>{decideError}</div>}
        <button type="button" className={styles.submit} disabled={deciding} onClick={() => decide(true)}>
          {deciding ? 'Connecting…' : 'Allow'}
        </button>
        <AuthButton variant="secondary" onClick={() => decide(false)}>Cancel</AuthButton>
      </div>
    </AuthShell>
  );
}
