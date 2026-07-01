import { FormEvent, useState } from 'react';
import { clearToken, getToken, setToken } from './api';
import Overview from './pages/Overview';
import Torrents from './pages/Torrents';

type Tab = 'overview' | 'torrents';

function LoginForm({ onLogin }: { onLogin: () => void }) {
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
      const res = await fetch('/api/v1/stats', {
        headers: { Authorization: `Bearer ${trimmed}` },
      });
      if (!res.ok) {
        clearToken();
        setError('Invalid token');
        return;
      }
      onLogin();
    } catch {
      clearToken();
      setError('Could not reach server');
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
  const [tab, setTab] = useState<Tab>('overview');

  function handleSignOut() {
    clearToken();
    setAuthenticated(false);
  }

  if (!authenticated) {
    return <LoginForm onLogin={() => setAuthenticated(true)} />;
  }

  return (
    <div className="app">
      <header className="header">
        <div className="header-brand">
          <span className="brand-mark">DN</span>
          <div>
            <h1>DebridNest</h1>
            <p className="header-subtitle">Dashboard</p>
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
        </nav>

        <button type="button" className="btn btn-ghost" onClick={handleSignOut}>
          Sign out
        </button>
      </header>

      <main className="main">
        {tab === 'overview' ? <Overview /> : <Torrents />}
      </main>
    </div>
  );
}
