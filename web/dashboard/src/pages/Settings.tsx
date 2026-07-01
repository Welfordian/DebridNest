import { FormEvent, useCallback, useEffect, useState } from 'react';
import {
  fetchConfig,
  fetchSettings,
  fetchSystem,
  joinUrl,
  patchSettings,
  purgeTorrents,
  runCleanup,
  type Config,
  type RetentionResult,
  type Settings,
  type SettingsPatch,
  type SystemInfo,
} from '../api';
import CopyButton from '../components/CopyButton';
import { useToast } from '../components/Toast';
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

function settingsToForm(settings: Settings) {
  return {
    retentionDays: String(settings.retentionDays ?? 0),
    diskQuotaGb: String(settings.diskQuotaGb ?? 0),
    downloadRateLimitMbps: String(settings.downloadRateLimitMbps ?? 0),
    webhookDiscordUrl: settings.webhookDiscordUrl ?? '',
    webhookNtfyTopic: settings.webhookNtfyTopic ?? '',
    webhookGotifyUrl: settings.webhookGotifyUrl ?? '',
    webhookGotifyToken: settings.webhookGotifyToken ?? '',
    notifyOnDownloadComplete: settings.notifyOnDownloadComplete ?? false,
    notifyOnQuotaWarning: settings.notifyOnQuotaWarning ?? false,
  };
}

interface SettingsProps {
  isAdmin: boolean;
}

export default function Settings({ isAdmin }: SettingsProps) {
  const { toast } = useToast();
  const [cleanupResult, setCleanupResult] = useState<RetentionResult | null>(null);
  const [cleanupError, setCleanupError] = useState<string | null>(null);
  const [cleanupBusy, setCleanupBusy] = useState(false);
  const [purgeBusy, setPurgeBusy] = useState<'completed' | 'failed' | null>(null);
  const [purgeMessage, setPurgeMessage] = useState<string | null>(null);
  const [saveBusy, setSaveBusy] = useState(false);

  const [form, setForm] = useState(() => ({
    retentionDays: '0',
    diskQuotaGb: '0',
    downloadRateLimitMbps: '0',
    webhookDiscordUrl: '',
    webhookNtfyTopic: '',
    webhookGotifyUrl: '',
    webhookGotifyToken: '',
    notifyOnDownloadComplete: false,
    notifyOnQuotaWarning: false,
  }));

  const configLoader = useCallback(async () => {
    const [config, system, settings] = await Promise.all([
      fetchConfig(),
      fetchSystem(),
      fetchSettings(),
    ]);
    return { config, system, settings };
  }, []);

  const { data, error, loading, refresh } = usePolling(configLoader, { intervalMs: 30000 });

  useEffect(() => {
    if (data?.settings) {
      setForm(settingsToForm(data.settings));
    }
  }, [data?.settings]);

  async function handleSave(e: FormEvent) {
    e.preventDefault();
    setSaveBusy(true);

    const patch: SettingsPatch = {
      retentionDays: Number(form.retentionDays) || 0,
      diskQuotaGb: Number(form.diskQuotaGb) || 0,
      downloadRateLimitMbps: Number(form.downloadRateLimitMbps) || 0,
      webhookDiscordUrl: form.webhookDiscordUrl.trim(),
      webhookNtfyTopic: form.webhookNtfyTopic.trim(),
      webhookGotifyUrl: form.webhookGotifyUrl.trim(),
      webhookGotifyToken: form.webhookGotifyToken.trim(),
      notifyOnDownloadComplete: form.notifyOnDownloadComplete,
      notifyOnQuotaWarning: form.notifyOnQuotaWarning,
    };

    try {
      await patchSettings(patch);
      toast('Settings saved');
      await refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Save failed', 'error');
    } finally {
      setSaveBusy(false);
    }
  }

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

  const { config, system } = data as {
    config: Config;
    system: SystemInfo;
    settings: Settings;
  };
  const urls = serviceUrls(config.publicUrl);

  return (
    <div className="settings">
      <form className="settings-editable" onSubmit={handleSave}>
        <section className="card config-card settings-form">
          <div className="card-heading">
            <h2>Limits & retention</h2>
            {isAdmin && (
              <button type="submit" className="btn btn-primary btn-sm" disabled={saveBusy}>
                {saveBusy ? 'Saving…' : 'Save changes'}
              </button>
            )}
          </div>
          <p className="muted section-desc">
            {isAdmin
              ? 'Adjust retention, disk quota, and download rate limit. Use 0 to disable a limit.'
              : 'Current limits (read-only). Contact an admin to change these values.'}
          </p>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="retention-days">Retention (days)</label>
              <input
                id="retention-days"
                className="input"
                type="number"
                min={0}
                value={form.retentionDays}
                disabled={!isAdmin}
                onChange={(e) => setForm((f) => ({ ...f, retentionDays: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="disk-quota">Disk quota (GB)</label>
              <input
                id="disk-quota"
                className="input"
                type="number"
                min={0}
                value={form.diskQuotaGb}
                disabled={!isAdmin}
                onChange={(e) => setForm((f) => ({ ...f, diskQuotaGb: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="rate-limit">Download cap (MB/s)</label>
              <input
                id="rate-limit"
                className="input"
                type="number"
                min={0}
                value={form.downloadRateLimitMbps}
                disabled={!isAdmin}
                onChange={(e) =>
                  setForm((f) => ({ ...f, downloadRateLimitMbps: e.target.value }))
                }
              />
            </div>
          </div>
        </section>

        <section className="card config-card settings-form">
          <div className="card-heading">
            <h2>Notifications</h2>
          </div>
          <p className="muted section-desc">
            Webhook targets and event toggles for download notifications.
          </p>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="discord-webhook">Discord webhook URL</label>
              <input
                id="discord-webhook"
                className="input"
                type="url"
                value={form.webhookDiscordUrl}
                disabled={!isAdmin}
                placeholder="https://discord.com/api/webhooks/…"
                onChange={(e) => setForm((f) => ({ ...f, webhookDiscordUrl: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="ntfy-topic">ntfy topic</label>
              <input
                id="ntfy-topic"
                className="input"
                value={form.webhookNtfyTopic}
                disabled={!isAdmin}
                placeholder="debridnest-alerts"
                onChange={(e) => setForm((f) => ({ ...f, webhookNtfyTopic: e.target.value }))}
              />
            </div>
          </div>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="gotify-url">Gotify URL</label>
              <input
                id="gotify-url"
                className="input"
                type="url"
                value={form.webhookGotifyUrl}
                disabled={!isAdmin}
                placeholder="https://gotify.example.com"
                onChange={(e) => setForm((f) => ({ ...f, webhookGotifyUrl: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="gotify-token">Gotify token</label>
              <input
                id="gotify-token"
                className="input"
                type="password"
                value={form.webhookGotifyToken}
                disabled={!isAdmin}
                autoComplete="off"
                onChange={(e) => setForm((f) => ({ ...f, webhookGotifyToken: e.target.value }))}
              />
            </div>
          </div>
          <div className="toggle-grid">
            <label className="toggle-label">
              <input
                type="checkbox"
                checked={form.notifyOnDownloadComplete}
                disabled={!isAdmin}
                onChange={(e) =>
                  setForm((f) => ({ ...f, notifyOnDownloadComplete: e.target.checked }))
                }
              />
              <span>Notify on download complete</span>
            </label>
            <label className="toggle-label">
              <input
                type="checkbox"
                checked={form.notifyOnQuotaWarning}
                disabled={!isAdmin}
                onChange={(e) =>
                  setForm((f) => ({ ...f, notifyOnQuotaWarning: e.target.checked }))
                }
              />
              <span>Notify when disk quota &gt;90%</span>
            </label>
          </div>
        </section>
      </form>

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
          <ConfigRow
            label="Started"
            value={system.startedAt ? new Date(system.startedAt).toLocaleString() : '—'}
          />
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
