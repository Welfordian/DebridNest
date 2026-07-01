import { FormEvent, useEffect, useState } from 'react';
import { clearToken, fetchMe, getToken, setToken, type Me } from './api';
import Activity from './pages/Activity';
import Library from './pages/Library';
import Logs from './pages/Logs';
import Overview from './pages/Overview';
import Settings from './pages/Settings';
import Torrents from './pages/Torrents';
import Users from './pages/Users';

export type Tab = 'overview' | 'torrents' | 'library' | 'settings' | 'users' | 'activity' | 'logs';

function LoginForm({ onLogin }: { onLogin: (me: Me) => void }) {
  const [token, setTokenInput] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = token.trim();
    if (!trimmed) {
      setError('Token is required');
      return;
    }

    setSubmitting(true);
    setToken(trimmed);

    try {
      const me = await fetchMe();
      onLogin(me);
    } catch {
      clearToken();
      setError('Invalid token');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="login">
      <div className="login-card card">
        <div className="brand">
          <span className="brand-mark">DN</span>
          <div>
            <h1>DebridNest</h1>
            <p className="muted">Self-hosted debrid control panel</p>
          </div>
        </div>
        <form onSubmit={handleSubmit}>
          <label htmlFor="token">API token</label>
          <input
            id="token"
            type="password"
            value={token}
            onChange={(e) => setTokenInput(e.target.value)}
            placeholder="DEBRIDNEST_API_TOKEN"
            autoComplete="off"
            autoFocus
          />
          {error && <p className="error">{error}</p>}
          <button type="submit" className="btn btn-primary" disabled={submitting}>
            {submitting ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  );
}

export default function App() {
  const [authenticated, setAuthenticated] = useState(() => !!getToken());
  const [me, setMe] = useState<Me | null>(null);
  const [tab, setTab] = useState<Tab>('overview');

  useEffect(() => {
    if (!authenticated) {
      setMe(null);
      return;
    }

    let cancelled = false;
    fetchMe()
      .then((profile) => {
        if (!cancelled) setMe(profile);
      })
      .catch(() => {
        if (!cancelled) {
          clearToken();
          setAuthenticated(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [authenticated]);

  function handleSignOut() {
    clearToken();
    setMe(null);
    setAuthenticated(false);
  }

  function handleLogin(profile: Me) {
    setMe(profile);
    setAuthenticated(true);
  }

  const isAdmin = me?.admin ?? false;

  if (!authenticated) {
    return <LoginForm onLogin={handleLogin} />;
  }

  return (
    <div className="app">
      <header className="header">
        <div className="header-brand">
          <span className="brand-mark">DN</span>
          <div>
            <h1>DebridNest</h1>
            <p className="header-subtitle">
              Dashboard
              {me && (
                <>
                  {' · '}
                  <span className="header-user">{me.name}</span>
                  {me.role && <span className="header-role muted"> ({me.role})</span>}
                </>
              )}
            </p>
          </div>
        </div>

        <nav className="tabs" aria-label="Dashboard sections">
          <button
            type="button"
            className={tab === 'overview' ? 'tab active' : 'tab'}
            onClick={() => setTab('overview')}
          >
            Overview
          </button>
          <button
            type="button"
            className={tab === 'torrents' ? 'tab active' : 'tab'}
            onClick={() => setTab('torrents')}
          >
            Torrents
          </button>
          <button
            type="button"
            className={tab === 'library' ? 'tab active' : 'tab'}
            onClick={() => setTab('library')}
          >
            Library
          </button>
          <button
            type="button"
            className={tab === 'settings' ? 'tab active' : 'tab'}
            onClick={() => setTab('settings')}
          >
            Settings
          </button>
          {isAdmin && (
            <>
              <button
                type="button"
                className={tab === 'users' ? 'tab active' : 'tab'}
                onClick={() => setTab('users')}
              >
                Users
              </button>
              <button
                type="button"
                className={tab === 'activity' ? 'tab active' : 'tab'}
                onClick={() => setTab('activity')}
              >
                Activity
              </button>
              <button
                type="button"
                className={tab === 'logs' ? 'tab active' : 'tab'}
                onClick={() => setTab('logs')}
              >
                Logs
              </button>
            </>
          )}
        </nav>

        <button type="button" className="btn btn-ghost" onClick={handleSignOut}>
          Sign out
        </button>
      </header>

      <main className="main">
        {tab === 'overview' && <Overview onNavigate={setTab} />}
        {tab === 'torrents' && <Torrents />}
        {tab === 'library' && <Library />}
        {tab === 'settings' && <Settings isAdmin={isAdmin} />}
        {tab === 'users' && isAdmin && <Users />}
        {tab === 'activity' && isAdmin && <Activity />}
        {tab === 'logs' && isAdmin && <Logs />}
      </main>
    </div>
  );
}
