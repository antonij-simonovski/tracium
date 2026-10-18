import type { AuthAppearance } from '../../../extensions';
import { FormEvent, useState } from 'react';
import { Link } from 'react-router-dom';
import { AuthShell } from '../components';
import { loginUser, requestErrorMessage } from '../api';
import styles from './LoginPage.module.css';

const errorMessages: Record<string, string> = {
  missing_fields: 'Please provide both your email and password to continue.',
  invalid_credentials: "We couldn't verify those credentials. Check your details and try again.",
};

interface LoginPageProps {
  appearance?: AuthAppearance;
  onLogin: (token: string, email: string) => void;
}

export default function LoginPage({ onLogin, appearance }: LoginPageProps) {
  const params = new URLSearchParams(window.location.search);
  const errorKey = params.get('error') ?? undefined;

  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(
    errorKey ? (errorMessages[errorKey] ?? 'Something went wrong. Please try again.') : null,
  );

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const email = (form.elements.namedItem('email') as HTMLInputElement).value;
    const password = (form.elements.namedItem('password') as HTMLInputElement).value;

    if (!email || !password) {
      setFormError(errorMessages.missing_fields);
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      const { token } = await loginUser({ email, password });
      onLogin(token, email);
    } catch (err) {
      setFormError(requestErrorMessage(err, { 401: errorMessages.invalid_credentials }));
    } finally {
      setSubmitting(false);
    }
  };

  const SignInOptions = appearance?.signInOptions;
  return (
    <AuthShell appearance={appearance}
      active="signin"
      title="Welcome back"
      subtitle="Sign in to your Tracium workspace."
      footnote={{ text: 'New to Tracium?', linkText: 'Create an account', to: '/signup' }}
    >
      {SignInOptions ? <SignInOptions mode="signin" /> : null}
      <form onSubmit={handleSubmit} className={styles.form}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="email">
            Email address
          </label>
          <input
            id="email"
            name="email"
            type="email"
            required
            autoComplete="email"
            className={styles.input}
            placeholder="you@example.com"
          />
        </div>

        <div className={styles.field}>
          <div className={styles.labelRow}>
            <label className={styles.label} htmlFor="password">
              Password
            </label>
            {appearance?.forgotPasswordHref ? (
              <Link to={appearance.forgotPasswordHref} className={styles.hint}>
                Forgot password?
              </Link>
            ) : null}
          </div>
          <input
            id="password"
            name="password"
            type="password"
            required
            autoComplete="current-password"
            className={`${styles.input} ${styles.password}`}
            placeholder="••••••••••••"
          />
        </div>

        {formError ? <div className={styles.errorBanner}>{formError}</div> : null}

        <button type="submit" className={styles.submit} disabled={submitting}>
          {submitting ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </AuthShell>
  );
}
