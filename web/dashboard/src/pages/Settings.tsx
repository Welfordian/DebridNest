import { FormEvent, useCallback, useEffect, useState } from 'react';
import {
  fetchConfig,
  fetchSettings,
  fetchSystem,
  joinUrl,
  patchSettings,
  purgeTorrents,
  runCleanup,
  testS3Settings,
  type Config,
  type RetentionResult,
  type Settings,
  type SettingsPatch,
  type SystemInfo,
} from '../api';
import CopyButton from '../components/CopyButton';
import Icon from '../components/Icon';
import Toggle from '../components/Toggle';
import { useToast } from '../components/Toast';
import { usePolling } from '../hooks/usePolling';
import { formatBytes, formatUptime } from '../lib/format';

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
    s3Enabled: settings.s3Enabled ?? false,
    s3Endpoint: settings.s3Endpoint ?? '',
    s3Bucket: settings.s3Bucket ?? '',
    s3Region: settings.s3Region ?? 'auto',
    s3Prefix: settings.s3Prefix ?? '',
    s3AccessKey: settings.s3AccessKey ?? '',
    s3SecretKey: settings.s3SecretKey ?? '',
    s3ForcePathStyle: settings.s3ForcePathStyle ?? false,
    s3OffloadLocal: settings.s3OffloadLocal ?? false,
  };
}

interface SettingsProps {
  isAdmin: boolean;
}

export default function SettingsPage({ isAdmin }: SettingsProps) {
  const { toast } = useToast();
  const [cleanupResult, setCleanupResult] = useState<RetentionResult | null>(null);
  const [cleanupError, setCleanupError] = useState<string | null>(null);
  const [cleanupBusy, setCleanupBusy] = useState(false);
  const [purgeBusy, setPurgeBusy] = useState<'completed' | 'failed' | null>(null);
  const [purgeMessage, setPurgeMessage] = useState<string | null>(null);
  const [saveBusy, setSaveBusy] = useState(false);
  const [s3TestBusy, setS3TestBusy] = useState(false);

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
    s3Enabled: false,
    s3Endpoint: '',
    s3Bucket: '',
    s3Region: 'auto',
    s3Prefix: '',
    s3AccessKey: '',
    s3SecretKey: '',
    s3ForcePathStyle: false,
    s3OffloadLocal: false,
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

    if (isAdmin) {
      patch.s3Enabled = form.s3Enabled;
      patch.s3Endpoint = form.s3Endpoint.trim();
      patch.s3Bucket = form.s3Bucket.trim();
      patch.s3Region = form.s3Region.trim() || 'auto';
      patch.s3Prefix = form.s3Prefix.trim();
      patch.s3ForcePathStyle = form.s3ForcePathStyle;
      patch.s3OffloadLocal = form.s3OffloadLocal;
      if (form.s3AccessKey.trim() && form.s3AccessKey !== '(configured)') {
        patch.s3AccessKey = form.s3AccessKey.trim();
      }
      if (form.s3SecretKey.trim() && form.s3SecretKey !== '(configured)') {
        patch.s3SecretKey = form.s3SecretKey.trim();
      }
    }

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

  async function handleS3Test() {
    setS3TestBusy(true);
    try {
      await testS3Settings();
      toast('S3 connection successful');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'S3 test failed', 'error');
    } finally {
      setS3TestBusy(false);
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
        <section className="card">
          <div className="card-heading">
            <h2>Limits &amp; retention</h2>
            {isAdmin && (
              <button type="submit" className="btn btn-primary btn-sm" disabled={saveBusy}>
                {saveBusy ? 'Saving…' : 'Save changes'}
              </button>
            )}
          </div>
          <p className="section-desc">
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
              <p className="form-hint">0 = unlimited</p>
            </div>
          </div>
        </section>

        <section className="card">
          <div className="card-heading">
            <h2>Notifications</h2>
          </div>
          <p className="section-desc">
            Webhook targets and event toggles for download notifications.
          </p>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="discord-webhook">Discord webhook URL</label>
              <input
                id="discord-webhook"
                className="input input-mono"
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
                className="input input-mono"
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
                className="input input-mono"
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
                className="input input-mono"
                type="password"
                value={form.webhookGotifyToken}
                disabled={!isAdmin}
                autoComplete="off"
                onChange={(e) => setForm((f) => ({ ...f, webhookGotifyToken: e.target.value }))}
              />
            </div>
          </div>
          <div className="toggle-grid">
            <Toggle
              checked={form.notifyOnDownloadComplete}
              disabled={!isAdmin}
              onChange={(checked) =>
                setForm((f) => ({ ...f, notifyOnDownloadComplete: checked }))
              }
              label="Notify on download complete"
            />
            <Toggle
              checked={form.notifyOnQuotaWarning}
              disabled={!isAdmin}
              onChange={(checked) => setForm((f) => ({ ...f, notifyOnQuotaWarning: checked }))}
              label={<span>Notify when disk quota &gt;90%</span>}
            />
          </div>
        </section>

        {isAdmin && (
          <section className="card">
            <div className="card-heading">
              <h2>Object storage (S3)</h2>
              <Toggle
                checked={form.s3Enabled}
                onChange={(checked) => setForm((f) => ({ ...f, s3Enabled: checked }))}
                label={form.s3Enabled ? 'Enabled' : 'Disabled'}
              />
            </div>
            <p className="section-desc">
              Upload completed files to S3-compatible storage (AWS S3, Cloudflare R2, Backblaze B2).
              Save settings before testing the connection.
            </p>
            <div
              style={{
                opacity: form.s3Enabled ? 1 : 0.45,
                pointerEvents: form.s3Enabled ? 'auto' : 'none',
              }}
            >
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="s3-endpoint">Endpoint</label>
                  <input
                    id="s3-endpoint"
                    className="input input-mono"
                    type="url"
                    value={form.s3Endpoint}
                    placeholder="https://accountid.r2.cloudflarestorage.com"
                    onChange={(e) => setForm((f) => ({ ...f, s3Endpoint: e.target.value }))}
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="s3-bucket">Bucket</label>
                  <input
                    id="s3-bucket"
                    className="input input-mono"
                    value={form.s3Bucket}
                    placeholder="debridnest"
                    onChange={(e) => setForm((f) => ({ ...f, s3Bucket: e.target.value }))}
                  />
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="s3-region">Region</label>
                  <input
                    id="s3-region"
                    className="input input-mono"
                    value={form.s3Region}
                    placeholder="auto"
                    onChange={(e) => setForm((f) => ({ ...f, s3Region: e.target.value }))}
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="s3-prefix">Key prefix</label>
                  <input
                    id="s3-prefix"
                    className="input input-mono"
                    value={form.s3Prefix}
                    placeholder="debridnest/"
                    onChange={(e) => setForm((f) => ({ ...f, s3Prefix: e.target.value }))}
                  />
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="s3-access-key">Access key</label>
                  <input
                    id="s3-access-key"
                    className="input input-mono"
                    type="password"
                    value={form.s3AccessKey}
                    placeholder="(configured)"
                    autoComplete="off"
                    onChange={(e) => setForm((f) => ({ ...f, s3AccessKey: e.target.value }))}
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="s3-secret-key">Secret key</label>
                  <input
                    id="s3-secret-key"
                    className="input input-mono"
                    type="password"
                    value={form.s3SecretKey}
                    placeholder="(configured)"
                    autoComplete="off"
                    onChange={(e) => setForm((f) => ({ ...f, s3SecretKey: e.target.value }))}
                  />
                </div>
              </div>
              <div className="toggle-grid" style={{ marginTop: 0, marginBottom: 14 }}>
                <Toggle
                  checked={form.s3ForcePathStyle}
                  onChange={(checked) => setForm((f) => ({ ...f, s3ForcePathStyle: checked }))}
                  label="Force path-style URLs"
                />
                <Toggle
                  checked={form.s3OffloadLocal}
                  onChange={(checked) => setForm((f) => ({ ...f, s3OffloadLocal: checked }))}
                  label="Delete local file after upload"
                />
              </div>
            </div>
            {form.s3Enabled && (
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={s3TestBusy}
                onClick={handleS3Test}
              >
                {s3TestBusy ? 'Testing…' : 'Test connection'}
              </button>
            )}
          </section>
        )}
      </form>

      <section className="card">
        <div className="card-heading">
          <h2>Service URLs</h2>
        </div>
        <div className="url-list">
          <UrlRow label="Public URL" url={urls.public} />
          <UrlRow label="WebDAV" url={urls.webdav} />
          <UrlRow label="Metrics" url={urls.metrics} />
          <UrlRow label="qBittorrent API (Sonarr/Radarr)" url={urls.qbit} />
          <UrlRow label="Stremio addon" url={urls.stremio} />
          <UrlRow label="Dashboard" url={urls.dashboard} />
        </div>
        <p className="hint-text">
          Stremio addon runs separately (default port 7001). Use the configure page to generate a
          personalized manifest URL.
        </p>
      </section>

      <section className="card">
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

      <section className="card">
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

      <section className="card">
        <div className="card-heading">
          <h2>Maintenance</h2>
        </div>
        <p className="section-desc">
          Run retention cleanup now, or purge finished torrents. Purges cannot be undone.
        </p>
        <div className="maintenance-actions">
          <button
            type="button"
            className="btn btn-secondary"
            disabled={cleanupBusy}
            onClick={handleCleanup}
          >
            <Icon name="rotate-cw" />
            {cleanupBusy ? 'Running cleanup…' : 'Run cleanup'}
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={purgeBusy !== null}
            onClick={() => handlePurge('completed')}
          >
            {purgeBusy === 'completed' ? 'Deleting…' : 'Purge completed'}
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={purgeBusy !== null}
            onClick={() => handlePurge('failed')}
          >
            {purgeBusy === 'failed' ? 'Deleting…' : 'Purge failed'}
          </button>
        </div>
        {cleanupResult && (
          <p className="success-msg">
            Cleanup complete — {cleanupResult.ageRemoved} removed by age,{' '}
            {cleanupResult.quotaRemoved} by quota. Disk: {formatBytes(cleanupResult.diskUsed)}
            {cleanupResult.diskQuota > 0 && ` / ${formatBytes(cleanupResult.diskQuota)}`}.
          </p>
        )}
        {cleanupError && <p className="error hint-text">{cleanupError}</p>}
        {purgeMessage && <p className="muted hint-text">{purgeMessage}</p>}
      </section>

      {error && <p className="error">{error}</p>}
    </div>
  );
}
