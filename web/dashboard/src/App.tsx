import { FormEvent, useCallback, useEffect, useState } from 'react';
import { clearToken, fetchMe, fetchStats, getToken, setToken, type Me } from './api';
import Icon from './components/Icon';
import { TopBarContext } from './components/TopBar';
import { usePolling } from './hooks/usePolling';
import Activity from './pages/Activity';
import Library from './pages/Library';
import Logs from './pages/Logs';
import Overview from './pages/Overview';
import Settings from './pages/Settings';
import Torrents from './pages/Torrents';
import Users from './pages/Users';

export type Tab = 'overview' | 'torrents' | 'library' | 'settings' | 'users' | 'activity' | 'logs';

const TITLES: Record<Tab, string> = {
  overview: 'Overview',
  torrents: 'Torrents',
  library: 'Library',
  settings: 'Settings',
  users: 'Users',
  activity: 'Activity',
  logs: 'Logs',
};

const NAV_ITEMS: { key: Tab; label: string; icon: string }[] = [
  { key: 'overview', label: 'Overview', icon: 'gauge' },
  { key: 'torrents', label: 'Torrents', icon: 'arrow-down-to-line' },
  { key: 'library', label: 'Library', icon: 'library' },
  { key: 'settings', label: 'Settings', icon: 'settings-2' },
];

const ADMIN_NAV_ITEMS: { key: Tab; label: string; icon: string }[] = [
  { key: 'users', label: 'Users', icon: 'users' },
  { key: 'activity', label: 'Activity', icon: 'activity' },
  { key: 'logs', label: 'Logs', icon: 'terminal' },
];

function BrandMark({ large = false, subtitle }: { large?: boolean; subtitle?: string }) {
  return (
    <span className={large ? 'brand brand-lg' : 'brand'}>
      <span className="brand-mark">DN</span>
      <span className="brand-text">
        <span className="brand-name">DebridNest</span>
        {subtitle && <span className="brand-subtitle">{subtitle}</span>}
      </span>
    </span>
  );
}

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
      <div className="login-card card card-hero">
        <BrandMark large subtitle="Self-hosted debrid control panel" />
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label htmlFor="token">API token</label>
            <input
              id="token"
              className={error ? 'input input-mono invalid' : 'input input-mono'}
              type="password"
              value={token}
              onChange={(e) => {
                setTokenInput(e.target.value);
                setError(null);
              }}
              placeholder="DEBRIDNEST_API_TOKEN"
              autoComplete="off"
              autoFocus
            />
          </div>
          {error && <p className="error">{error}</p>}
          <button
            type="submit"
            className="btn btn-primary btn-block"
            disabled={submitting || !token.trim()}
          >
            {submitting ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  );
}

function NavItem({
  item,
  active,
  count,
  onSelect,
}: {
  item: { key: Tab; label: string; icon: string };
  active: boolean;
  count?: number;
  onSelect: (tab: Tab) => void;
}) {
  return (
    <button
      type="button"
      className={active ? 'nav-item active' : 'nav-item'}
      onClick={() => onSelect(item.key)}
    >
      <Icon name={item.icon} size={16} />
      {item.label}
      {count != null && <span className="nav-count">{count}</span>}
    </button>
  );
}

export default function App() {
  const [authenticated, setAuthenticated] = useState(() => !!getToken());
  const [me, setMe] = useState<Me | null>(null);
  const [tab, setTab] = useState<Tab>('overview');
  const [metaEl, setMetaEl] = useState<HTMLElement | null>(null);
  const [actionsEl, setActionsEl] = useState<HTMLElement | null>(null);

  const statsLoader = useCallback(() => fetchStats(), []);
  const { data: stats } = usePolling(statsLoader, {
    intervalMs: 15000,
    enabled: authenticated,
  });

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
    <div className="layout">
      <div className="sidebar">
        <div className="sidebar-brand">
          <BrandMark />
        </div>
        <nav className="nav" aria-label="Dashboard sections">
          {NAV_ITEMS.map((item) => (
            <NavItem
              key={item.key}
              item={item}
              active={tab === item.key}
              count={item.key === 'torrents' ? stats?.torrentCount : undefined}
              onSelect={setTab}
            />
          ))}
          {isAdmin && (
            <>
              <div className="nav-section-label">Admin</div>
              {ADMIN_NAV_ITEMS.map((item) => (
                <NavItem key={item.key} item={item} active={tab === item.key} onSelect={setTab} />
              ))}
            </>
          )}
        </nav>
        <div className="sidebar-footer">
          <div className="sidebar-user">
            <span className="sidebar-user-info">
              <span className="sidebar-user-name">{me?.name ?? '—'}</span>
              <span className="sidebar-user-role">{me?.role ? `${me.role} role` : ''}</span>
            </span>
            <button type="button" className="btn btn-ghost btn-sm" onClick={handleSignOut}>
              <Icon name="log-out" size={14} />
              Sign out
            </button>
          </div>
        </div>
      </div>

      <div className="content">
        <header className="topbar">
          <h1 className="topbar-title">{TITLES[tab]}</h1>
          <span className="topbar-meta" ref={setMetaEl} />
          <div className="topbar-actions" ref={setActionsEl} />
        </header>
        <main className="main">
          <TopBarContext.Provider value={{ metaEl, actionsEl }}>
            {tab === 'overview' && <Overview onNavigate={setTab} />}
            {tab === 'torrents' && <Torrents />}
            {tab === 'library' && <Library />}
            {tab === 'settings' && <Settings isAdmin={isAdmin} />}
            {tab === 'users' && isAdmin && <Users />}
            {tab === 'activity' && isAdmin && <Activity />}
            {tab === 'logs' && isAdmin && <Logs />}
          </TopBarContext.Provider>
        </main>
      </div>
    </div>
  );
}
