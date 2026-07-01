import { useCallback } from 'react';
import { fetchStats, joinUrl } from '../api';
import type { Tab } from '../App';
import { usePolling } from '../hooks/usePolling';
import {
  formatBytes,
  formatQuotaLabel,
  formatRelativeTime,
  formatSpeed,
} from '../lib/format';

function statusTotal(counts: Record<string, number> | undefined, keys: string[]): number {
  if (!counts) return 0;
  return keys.reduce((sum, key) => sum + (counts[key] ?? 0), 0);
}

function ConfigRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="config-row">
      <span className="config-label">{label}</span>
      <span className="config-value">{value}</span>
    </div>
  );
}

function QuickLink({
  label,
  description,
  onClick,
  href,
  external,
}: {
  label: string;
  description: string;
  onClick?: () => void;
  href?: string;
  external?: boolean;
}) {
  if (href) {
    return (
      <a
        className="quick-link"
        href={href}
        target={external ? '_blank' : undefined}
        rel={external ? 'noreferrer' : undefined}
      >
        <span className="quick-link-label">{label}</span>
        <span className="muted quick-link-desc">{description}</span>
      </a>
    );
  }

  return (
    <button type="button" className="quick-link" onClick={onClick}>
      <span className="quick-link-label">{label}</span>
      <span className="muted quick-link-desc">{description}</span>
    </button>
  );
}

export default function Overview({ onNavigate }: { onNavigate: (tab: Tab) => void }) {
  const loader = useCallback(() => fetchStats(), []);
  const { data: stats, error, loading, updatedAt, refresh } = usePolling(loader);

  if (loading && !stats) {
    return <p className="muted page-loading">Loading overview…</p>;
  }

  if (error && !stats) {
    return (
      <div className="page-error card">
        <p className="error">{error}</p>
        <button type="button" className="btn btn-secondary" onClick={() => refresh()}>
          Retry
        </button>
      </div>
    );
  }

  if (!stats) return null;

  const hasQuota = stats.diskQuota > 0;
  const usedPct = hasQuota ? Math.min(100, (stats.diskUsed / stats.diskQuota) * 100) : 0;
  const completed = statusTotal(stats.statusCounts, ['downloaded']);
  const downloading = statusTotal(stats.statusCounts, [
    'downloading',
    'queued',
    'waiting_files_selection',
    'magnet_conversion',
  ]);
  const failed = statusTotal(stats.statusCounts, ['error', 'dead']);

  const base = stats.publicUrl.replace(/\/+$/, '');

  return (
    <div className="overview">
      <div className="page-toolbar">
        <p className="muted toolbar-meta">
          {updatedAt ? `Updated ${formatRelativeTime(updatedAt)}` : 'Live stats'}
        </p>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
          Refresh
        </button>
      </div>

      <section className="hero-grid">
        <article className="card hero-card">
          <div className="card-heading">
            <h2>Storage</h2>
            {!hasQuota && <span className="pill pill-muted">Unlimited</span>}
          </div>
          <p className="hero-value">{formatBytes(stats.diskUsed)}</p>
          <div className="disk-bar" aria-hidden={!hasQuota}>
            <div
              className="disk-bar-fill"
              style={{ width: hasQuota ? `${usedPct}%` : '0%' }}
            />
          </div>
          <p className="stat-detail">{formatQuotaLabel(stats.diskUsed, stats.diskQuota, stats.diskQuotaGb)}</p>
        </article>

        <article className="card hero-card">
          <div className="card-heading">
            <h2>Download speed</h2>
            <span className="pill pill-live">Live</span>
          </div>
          <p className="hero-value">{formatSpeed(stats.downloadSpeed)}</p>
          <div className="disk-bar disk-bar-spacer" aria-hidden="true">
            <div className="disk-bar-fill" style={{ width: '0%' }} />
          </div>
          <p className="stat-detail muted">
            {stats.activeCount} active · {stats.torrentCount} total torrents
          </p>
        </article>
      </section>

      <section className="stat-grid">
        <article className="card stat-card">
          <h3>Active</h3>
          <p className="stat-value">{stats.activeCount}</p>
          <p className="stat-detail muted">In progress or queued</p>
        </article>
        <article className="card stat-card">
          <h3>Downloading</h3>
          <p className="stat-value">{downloading}</p>
          <p className="stat-detail muted">Including selection stage</p>
        </article>
        <article className="card stat-card">
          <h3>Completed</h3>
          <p className="stat-value">{completed}</p>
          <p className="stat-detail muted">Ready to stream</p>
        </article>
        <article className="card stat-card">
          <h3>Failed</h3>
          <p className="stat-value">{failed}</p>
          <p className="stat-detail muted">Error or dead</p>
        </article>
      </section>

      <section className="card config-card">
        <div className="card-heading">
          <h2>Quick links</h2>
        </div>
        <div className="quick-links">
          <QuickLink
            label="Library"
            description="Completed torrents, WebDAV paths, stream links"
            onClick={() => onNavigate('library')}
          />
          <QuickLink
            label="Settings"
            description="Service URLs, maintenance, purge"
            onClick={() => onNavigate('settings')}
          />
          {stats.webdavEnabled && base && (
            <QuickLink
              label="WebDAV"
              description="Browse completed files"
              href={joinUrl(base, '/webdav/')}
              external
            />
          )}
          {stats.metricsEnabled && base && (
            <QuickLink
              label="Metrics"
              description="Prometheus /metrics endpoint"
              href={joinUrl(base, '/metrics')}
              external
            />
          )}
          {base && (
            <QuickLink
              label="Dashboard"
              description="This control panel"
              href={joinUrl(base, '/dashboard/')}
            />
          )}
        </div>
      </section>

      <section className="card config-card">
        <div className="card-heading">
          <h2>Configuration</h2>
        </div>
        <div className="config-grid">
          <ConfigRow label="Public URL" value={stats.publicUrl || '—'} />
          <ConfigRow
            label="Retention"
            value={stats.retentionDays > 0 ? `${stats.retentionDays} days` : 'Disabled'}
          />
          <ConfigRow
            label="Disk quota"
            value={stats.diskQuotaGb > 0 ? `${stats.diskQuotaGb} GB` : 'Not set'}
          />
          <ConfigRow
            label="Download cap"
            value={stats.rateLimitMbps > 0 ? `${stats.rateLimitMbps} MB/s` : 'Unlimited'}
          />
          <ConfigRow label="WebDAV" value={stats.webdavEnabled ? 'Enabled' : 'Disabled'} />
          <ConfigRow label="Metrics" value={stats.metricsEnabled ? 'Enabled (/metrics)' : 'Disabled'} />
        </div>
      </section>

      {error && <p className="error">{error}</p>}
    </div>
  );
}
