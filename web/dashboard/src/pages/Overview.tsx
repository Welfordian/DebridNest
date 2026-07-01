import { useEffect, useState } from 'react';
import { fetchStats, type Stats } from '../api';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** i;
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function formatSpeed(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`;
}

export default function Overview() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const data = await fetchStats();
        if (!cancelled) {
          setStats(data);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load stats');
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    const interval = setInterval(load, 5000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  if (loading && !stats) {
    return <p className="muted">Loading stats…</p>;
  }

  if (error && !stats) {
    return <p className="error">{error}</p>;
  }

  if (!stats) return null;

  const quota = stats.diskQuota || 1;
  const usedPct = Math.min(100, (stats.diskUsed / quota) * 100);

  return (
    <div className="overview">
      <section className="card">
        <h2>Disk usage</h2>
        <div className="disk-bar">
          <div className="disk-bar-fill" style={{ width: `${usedPct}%` }} />
        </div>
        <p className="stat-detail">
          {formatBytes(stats.diskUsed)} / {formatBytes(stats.diskQuota)}
          <span className="muted"> ({usedPct.toFixed(1)}%)</span>
        </p>
      </section>

      <div className="stat-grid">
        <section className="card stat-card">
          <h3>Active torrents</h3>
          <p className="stat-value">{stats.activeCount}</p>
          <p className="stat-detail muted">{stats.torrentCount} total</p>
        </section>

        <section className="card stat-card">
          <h3>Download speed</h3>
          <p className="stat-value">{formatSpeed(stats.downloadSpeed)}</p>
        </section>

        <section className="card stat-card">
          <h3>Retention</h3>
          <p className="stat-value">{stats.retentionDays}</p>
          <p className="stat-detail muted">days</p>
        </section>
      </div>

      {error && <p className="error">{error}</p>}
    </div>
  );
}
