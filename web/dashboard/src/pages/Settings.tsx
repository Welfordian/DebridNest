import { useCallback, useState } from 'react';
import {
  fetchConfig,
  fetchSystem,
  joinUrl,
  purgeTorrents,
  runCleanup,
  type Config,
  type RetentionResult,
  type SystemInfo,
} from '../api';
import CopyButton from '../components/CopyButton';
import { usePolling } from '../hooks/usePolling';
import { formatBytes, formatUptime } from '../lib/format';

function ConfigRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="config-row">
      <span className="config-label">{label}</span>
      <span className="config-value">{value}</span>
    </div>
  );
}

function UrlRow({ label, url }: { label: string; url: string }) {
  return (
    <div className="url-row">
      <div className="url-row-text">
        <span className="config-label">{label}</span>
        <code className="url-value">{url || '—'}</code>
      </div>
      <CopyButton value={url} label="Copy" />
    </div>
  );
}

function serviceUrls(publicUrl: string) {
  const base = publicUrl.replace(/\/+$/, '');
  return {
    public: base,
    webdav: joinUrl(base, '/webdav/'),
    metrics: joinUrl(base, '/metrics'),
    qbit: joinUrl(base, '/api/v2'),
    stremio: 'http://127.0.0.1:7001/manifest.json',
    dashboard: joinUrl(base, '/dashboard/'),
  };
}

export default function Settings() {
  const [cleanupResult, setCleanupResult] = useState<RetentionResult | null>(null);
  const [cleanupError, setCleanupError] = useState<string | null>(null);
  const [cleanupBusy, setCleanupBusy] = useState(false);
  const [purgeBusy, setPurgeBusy] = useState<'completed' | 'failed' | null>(null);
  const [purgeMessage, setPurgeMessage] = useState<string | null>(null);

  const configLoader = useCallback(async () => {
    const [config, system] = await Promise.all([fetchConfig(), fetchSystem()]);
    return { config, system };
  }, []);

  const { data, error, loading, refresh } = usePolling(configLoader, { intervalMs: 30000 });

  async function handleCleanup() {
    setCleanupBusy(true);
    setCleanupError(null);
    setCleanupResult(null);
    try {
      const result = await runCleanup();
      setCleanupResult(result);
      await refresh();
    } catch (err) {
      setCleanupError(err instanceof Error ? err.message : 'Cleanup failed');
    } finally {
      setCleanupBusy(false);
    }
  }

  async function handlePurge(filter: 'completed' | 'failed') {
    const label = filter === 'completed' ? 'all completed' : 'all failed';
    if (!confirm(`Delete ${label} torrents and their files? This cannot be undone.`)) return;

    setPurgeBusy(filter);
    setPurgeMessage(null);
    try {
      const result = await purgeTorrents(filter);
      setPurgeMessage(`Removed ${result.deleted} torrent${result.deleted === 1 ? '' : 's'}.`);
      await refresh();
    } catch (err) {
      setPurgeMessage(err instanceof Error ? err.message : 'Purge failed');
    } finally {
      setPurgeBusy(null);
    }
  }

  if (loading && !data) {
    return <p className="muted page-loading">Loading settings…</p>;
  }

  if (error && !data) {
    return (
      <div className="page-error card">
        <p className="error">{error}</p>
        <button type="button" className="btn btn-secondary" onClick={() => refresh()}>
          Retry
        </button>
      </div>
    );
  }

  if (!data) return null;

  const { config, system } = data as { config: Config; system: SystemInfo };
  const urls = serviceUrls(config.publicUrl);

  return (
    <div className="settings">
      <section className="card config-card">
        <div className="card-heading">
          <h2>Service URLs</h2>
        </div>
        <div className="url-list">
          <UrlRow label="Public URL" url={urls.public} />
          <UrlRow label="WebDAV URL" url={urls.webdav} />
          <UrlRow label="Metrics URL" url={urls.metrics} />
          <UrlRow label="qBit API URL" url={urls.qbit} />
          <UrlRow label="Stremio addon URL" url={urls.stremio} />
          <UrlRow label="Dashboard URL" url={urls.dashboard} />
        </div>
        <p className="muted hint-text">
          Stremio addon runs separately (default port 7001). Use the configure page to generate a
          personalized manifest URL.
        </p>
      </section>

      <section className="card config-card">
        <div className="card-heading">
          <h2>Configuration</h2>
        </div>
        <div className="config-grid">
          <ConfigRow label="Public URL" value={config.publicUrl || '—'} />
          <ConfigRow
            label="Retention"
            value={config.retentionDays > 0 ? `${config.retentionDays} days` : 'Disabled'}
          />
          <ConfigRow
            label="Disk quota"
            value={config.diskQuotaGb > 0 ? `${config.diskQuotaGb} GB` : 'Not set'}
          />
          <ConfigRow
            label="Download cap"
            value={config.rateLimitMbps > 0 ? `${config.rateLimitMbps} MB/s` : 'Unlimited'}
          />
          <ConfigRow label="WebDAV" value={config.webdavEnabled ? 'Enabled' : 'Disabled'} />
          <ConfigRow label="Metrics" value={config.metricsEnabled ? 'Enabled' : 'Disabled'} />
          {config.linkTtlHours != null && (
            <ConfigRow label="Link TTL" value={`${config.linkTtlHours} hours`} />
          )}
          {config.autoSelectSeconds != null && (
            <ConfigRow label="Auto-select" value={`${config.autoSelectSeconds}s`} />
          )}
          {config.qbitUser && <ConfigRow label="qBit user" value={config.qbitUser} />}
          {config.seedAfterComplete != null && (
            <ConfigRow label="Seeding" value={config.seedAfterComplete ? 'Enabled' : 'Disabled'} />
          )}
          {config.transcodeEnabled != null && (
            <ConfigRow label="Transcode" value={config.transcodeEnabled ? 'Enabled' : 'Disabled'} />
          )}
        </div>
      </section>

      <section className="card config-card">
        <div className="card-heading">
          <h2>System</h2>
        </div>
        <div className="config-grid">
          <ConfigRow label="Version" value={system.version || '—'} />
          <ConfigRow label="Listen" value={system.listen || '—'} />
          <ConfigRow label="Torrent port" value={String(system.torrentPort ?? '—')} />
          <ConfigRow label="Uptime" value={formatUptime(system.uptimeSeconds)} />
          <ConfigRow label="Started" value={system.startedAt ? new Date(system.startedAt).toLocaleString() : '—'} />
        </div>
        {system.features && Object.keys(system.features).length > 0 && (
          <div className="feature-tags">
            {Object.entries(system.features).map(([key, enabled]) => (
              <span key={key} className={enabled ? 'pill pill-live' : 'pill pill-muted'}>
                {key.replace(/_/g, ' ')}
              </span>
            ))}
          </div>
        )}
      </section>

      <section className="card config-card">
        <div className="card-heading">
          <h2>Maintenance</h2>
        </div>
        <p className="muted section-desc">
          Run retention cleanup immediately — removes torrents past retention age and enforces disk
          quota.
        </p>
        <button
          type="button"
          className="btn btn-secondary"
          disabled={cleanupBusy}
          onClick={handleCleanup}
        >
          {cleanupBusy ? 'Running cleanup…' : 'Run retention cleanup now'}
        </button>
        {cleanupResult && (
          <p className="success-msg">
            Cleanup complete — {cleanupResult.ageRemoved} removed by age,{' '}
            {cleanupResult.quotaRemoved} by quota. Disk: {formatBytes(cleanupResult.diskUsed)}
            {cleanupResult.diskQuota > 0 && ` / ${formatBytes(cleanupResult.diskQuota)}`}.
          </p>
        )}
        {cleanupError && <p className="error">{cleanupError}</p>}

        <div className="maintenance-actions">
          <button
            type="button"
            className="btn btn-danger"
            disabled={purgeBusy !== null}
            onClick={() => handlePurge('completed')}
          >
            {purgeBusy === 'completed' ? 'Deleting…' : 'Delete all completed'}
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={purgeBusy !== null}
            onClick={() => handlePurge('failed')}
          >
            {purgeBusy === 'failed' ? 'Deleting…' : 'Delete all failed'}
          </button>
        </div>
        {purgeMessage && <p className="muted">{purgeMessage}</p>}
      </section>

      {error && <p className="error">{error}</p>}
    </div>
  );
}
