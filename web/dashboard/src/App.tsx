import { FormEvent, useState } from 'react';
import { getToken, setToken } from './api';
import Overview from './pages/Overview';
import Torrents from './pages/Torrents';

type Tab = 'overview' | 'torrents';

function LoginForm({ onLogin }: { onLogin: () => void }) {
  const [token, setTokenInput] = useState('');
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = token.trim();
    if (!trimmed) {
      setError('Token is required');
      return;
    }

    setToken(trimmed);

    try {
      const res = await fetch('/api/v1/stats', {
        headers: { Authorization: `Bearer ${trimmed}` },
      });
      if (!res.ok) {
        localStorage.removeItem('debridnest_token');
        setError('Invalid token');
        return;
      }
      onLogin();
    } catch {
      localStorage.removeItem('debridnest_token');
      setError('Could not reach server');
    }
  }

  return (
    <div className="login">
      <div className="login-card card">
        <h1>DebridNest</h1>
        <p className="muted">Enter your API token to continue.</p>
        <form onSubmit={handleSubmit}>
          <label htmlFor="token">API token</label>
          <input
            id="token"
            type="password"
            value={token}
            onChange={(e) => setTokenInput(e.target.value)}
            placeholder="Bearer token"
            autoComplete="off"
            autoFocus
          />
          {error && <p className="error">{error}</p>}
          <button type="submit" className="btn btn-primary">
            Sign in
          </button>
        </form>
      </div>
    </div>
  );
}

export default function App() {
  const [authenticated, setAuthenticated] = useState(() => !!getToken());
  const [tab, setTab] = useState<Tab>('overview');

  if (!authenticated) {
    return <LoginForm onLogin={() => setAuthenticated(true)} />;
  }

  return (
    <div className="app">
      <header className="header">
        <h1>DebridNest Dashboard</h1>
        <nav className="tabs">
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
      </header>

      <main className="main">
        {tab === 'overview' ? <Overview /> : <Torrents />}
      </main>
    </div>
  );
}
