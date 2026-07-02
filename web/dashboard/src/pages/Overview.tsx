import { ReactNode, useCallback } from 'react';
import { fetchStats, joinUrl } from '../api';
import type { Tab } from '../App';
import Icon from '../components/Icon';
import { TopBarActions, TopBarMeta } from '../components/TopBar';
import { usePolling } from '../hooks/usePolling';
import {
  formatBytes,
  formatQuotaLabel,
  formatRelativeTime,
  formatSpeed,
} from '../lib/format';
import { lifecycleCount } from '../lib/torrentLifecycle';

function ConfigRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="config-row">
      <div className="config-row-text">
        <span className="config-label">{label}</span>
        <span className="config-value">{value}</span>
      </div>
    </div>
  );
}

function formatCount(value: number, singular: string, plural: string) {
  const safeValue = Number.isFinite(value) ? value : 0;
  return `${new Intl.NumberFormat().format(safeValue)} ${safeValue === 1 ? singular : plural}`;
}

function StatCard({
  label,
  value,
  detail,
  hero = false,
  accessory,
  children,
}: {
  label: string;
  value: string;
  detail?: string;
  hero?: boolean;
  accessory?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <article className={hero ? 'card card-hero stat-card hero-card' : 'card stat-card'}>
      <div className="stat-card-top">
        <span className="stat-label">{label}</span>
        {accessory}
      </div>
      <p className="stat-value">{value}</p>
      {children}
      {detail && <p className="stat-detail">{detail}</p>}
    </article>
  );
}

function QuickLink({
  label,
  description,
  icon,
  onClick,
  href,
  external,
}: {
  label: string;
  description: string;
  icon: string;
  onClick?: () => void;
  href?: string;
  external?: boolean;
}) {
  const inner = (
    <>
      <span className="quick-link-icon">
        <Icon name={icon} size={18} />
      </span>
      <span className="quick-link-text">
        <span className="quick-link-label">
          {label}
          {external && <Icon name="external-link" size={12} />}
        </span>
        <span className="quick-link-desc">{description}</span>
      </span>
    </>
  );

  if (href) {
    return (
      <a
        className="quick-link"
        href={href}
        target={external ? '_blank' : undefined}
        rel={external ? 'noreferrer' : undefined}
      >
        {inner}
      </a>
    );
  }

  return (
    <button type="button" className="quick-link" onClick={onClick}>
      {inner}
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
  const quotaTone = usedPct >= 95 ? ' tone-danger' : usedPct >= 80 ? ' tone-streamable' : '';
  const s3Used = Number.isFinite(stats.s3Used) ? stats.s3Used : 0;
  const s3Quota = Number.isFinite(stats.s3Quota) ? stats.s3Quota : 0;
  const s3QuotaGb = Number.isFinite(stats.s3QuotaGb) ? stats.s3QuotaGb : 0;
  const s3ObjectCount = Number.isFinite(stats.s3ObjectCount) ? stats.s3ObjectCount : 0;
  const hasS3Quota = stats.s3Enabled && s3Quota > 0;
  const s3UsedPct = hasS3Quota ? Math.min(100, (s3Used / s3Quota) * 100) : 0;
  const s3QuotaTone =
    s3UsedPct >= 95 ? ' tone-danger' : s3UsedPct >= 80 ? ' tone-streamable' : '';
  const s3ObjectLabel = formatCount(s3ObjectCount, 'object', 'objects');
  const s3Detail = stats.s3Enabled
    ? `${formatQuotaLabel(s3Used, s3Quota, s3QuotaGb)} · ${s3ObjectLabel}`
    : `Disabled · ${s3ObjectLabel}`;
  const active = lifecycleCount(stats.lifecycleCounts, stats.statusCounts, 'active');
  const ready = lifecycleCount(stats.lifecycleCounts, stats.statusCounts, 'completed');
  const failed = lifecycleCount(stats.lifecycleCounts, stats.statusCounts, 'failed');
  const other = lifecycleCount(stats.lifecycleCounts, stats.statusCounts, 'other');

  const base = stats.publicUrl.replace(/\/+$/, '');

  return (
    <div className="page">
      <TopBarMeta>
        {updatedAt ? `updated ${formatRelativeTime(updatedAt)}` : 'Live stats'}
      </TopBarMeta>
      <TopBarActions>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
          <Icon name="rotate-cw" size={14} />
          Refresh
        </button>
      </TopBarActions>

      <section className="hero-grid">
        <StatCard
          hero
          label="Storage"
          value={formatBytes(stats.diskUsed)}
          detail={formatQuotaLabel(stats.diskUsed, stats.diskQuota, stats.diskQuotaGb)}
          accessory={
            hasQuota ? (
              <span className={usedPct >= 80 ? 'pill pill-warning' : 'pill pill-accent'}>
                {usedPct.toFixed(0)}% of quota
              </span>
            ) : (
              <span className="pill pill-muted">Unlimited</span>
            )
          }
        >
          <div className="disk-bar" aria-hidden={!hasQuota}>
            <div
              className={`disk-bar-fill${quotaTone}`}
              style={{ width: hasQuota ? `${usedPct}%` : '0%' }}
            />
          </div>
        </StatCard>

        <StatCard
          hero
          label="Object storage"
          value={formatBytes(s3Used)}
          detail={s3Detail}
          accessory={
            stats.s3Enabled ? (
              hasS3Quota ? (
                <span className={s3UsedPct >= 80 ? 'pill pill-warning' : 'pill pill-accent'}>
                  {s3UsedPct.toFixed(0)}% of quota
                </span>
              ) : (
                <span className="pill pill-accent">Unlimited</span>
              )
            ) : (
              <span className="pill pill-muted">Disabled</span>
            )
          }
        >
          <div className="disk-bar" aria-hidden={!hasS3Quota}>
            <div
              className={`disk-bar-fill${s3QuotaTone}`}
              style={{ width: hasS3Quota ? `${s3UsedPct}%` : '0%' }}
            />
          </div>
        </StatCard>

        <StatCard
          hero
          label="Download speed"
          value={formatSpeed(stats.downloadSpeed)}
          detail={`${active} active · ${stats.torrentCount} total torrents`}
          accessory={<span className="pill pill-live">Live</span>}
        />
      </section>

      <section className="stat-grid">
        <StatCard label="Active" value={String(active)} detail="In progress or queued" />
        <StatCard label="Ready" value={String(ready)} detail="Completed and link-visible" />
        <StatCard label="Failed" value={String(failed)} detail="Errors, dead torrents, or magnet failures" />
        <StatCard label="Other" value={String(other)} detail="Unclassified lifecycle states" />
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>Quick links</h2>
        </div>
        <div className="quick-links">
          <QuickLink
            icon="library"
            label="Library"
            description="Completed torrents, WebDAV paths, stream links"
            onClick={() => onNavigate('library')}
          />
          <QuickLink
            icon="settings-2"
            label="Settings"
            description="Limits, notifications, maintenance"
            onClick={() => onNavigate('settings')}
          />
          {stats.webdavEnabled && base && (
            <QuickLink
              icon="hard-drive"
              label="WebDAV"
              description="Browse completed files"
              href={joinUrl(base, '/webdav/')}
              external
            />
          )}
          {stats.metricsEnabled && base && (
            <QuickLink
              icon="activity"
              label="Metrics"
              description="Prometheus /metrics endpoint"
              href={joinUrl(base, '/metrics')}
              external
            />
          )}
        </div>
      </section>

      <section className="card">
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
          <ConfigRow label="Object storage" value={stats.s3Enabled ? 'Enabled' : 'Disabled'} />
          <ConfigRow
            label="S3 quota"
            value={stats.s3QuotaGb > 0 ? `${stats.s3QuotaGb} GB` : 'Not set'}
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
