import { useState, type FormEvent } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { LoaderCircle, LogIn, Moon, Sun } from 'lucide-react';
import { api } from '../api/endpoints';
import { errorMessage } from '../api/client';
import { useStatus } from '../app/session';
import { BrandMark, Field, Notice } from '../components/ui';
import { useTheme } from '../lib/theme';

export default function Login() {
  const { status } = useStatus();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [theme, toggleTheme] = useTheme();

  const next = params.get('next');
  const target = next && next.startsWith('/') && !next.startsWith('//') ? next : '/';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!username.trim() || !password) {
      setError('Podaj nazwę użytkownika i hasło.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.login(username.trim(), password);
      navigate(target, { replace: true });
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <div className="auth">
      <button
        type="button"
        className="icon-btn auth-theme"
        onClick={toggleTheme}
        aria-label={theme === 'dark' ? 'Jasny motyw' : 'Ciemny motyw'}
      >
        {theme === 'dark' ? <Sun size={20} /> : <Moon size={20} />}
      </button>
      <div className="auth-inner">
        <div className="auth-brand">
          <BrandMark className="auth-mark" />
          <div>
            <div className="auth-title">ChameleonTAK</div>
            <div className="muted">{status.displayName || status.serverName}</div>
          </div>
        </div>
        <form className="plate auth-card stack" onSubmit={submit} noValidate>
          <h1 className="auth-heading">Logowanie do panelu</h1>
          {error && <Notice kind="danger">{error}</Notice>}
          <Field label="Nazwa użytkownika" htmlFor="login-user">
            <input
              id="login-user"
              className="input"
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoFocus
            />
          </Field>
          <Field label="Hasło" htmlFor="login-pass">
            <input
              id="login-pass"
              className="input"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
          <button type="submit" className="btn btn-primary btn-lg btn-block" disabled={busy}>
            {busy ? <LoaderCircle size={20} className="spinner" /> : <LogIn size={20} />}
            Zaloguj
          </button>
        </form>
        <p className="xsmall faint auth-foot">
          Nie pamiętasz hasła? Na serwerze uruchom <code>chameleontak reset-admin --password NOWE</code>.
        </p>
      </div>
    </div>
  );
}
