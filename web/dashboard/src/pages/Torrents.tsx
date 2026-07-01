import { useCallback, useMemo, useState } from 'react';
import {
  deleteTorrent,
  fetchTorrents,
  retryTorrent,
  type Torrent,
} from '../api';
import { usePolling } from '../hooks/usePolling';
import {
  formatBytes,
  formatProgress,
  formatRelativeTime,
  formatSpeed,
} from '../lib/format';

type Filter = 'all' | 'active' | 'completed' | 'failed';

const ACTIVE_STATUSES = new Set([
  'downloading',
  'queued',
  'waiting_files_selection',
  'magnet_conversion',
]);
const COMPLETED_STATUSES = new Set(['downloaded']);
const FAILED_STATUSES = new Set(['error', 'dead']);

function matchesFilter(torrent: Torrent, filter: Filter): boolean {
  switch (filter) {
    case 'active':
      return ACTIVE_STATUSES.has(torrent.status);
    case 'completed':
      return COMPLETED_STATUSES.has(torrent.status);
    case 'failed':
      return FAILED_STATUSES.has(torrent.status);
    default:
      return true;
  }
}

function ProgressCell({ progress, status }: { progress: number; status: string }) {
  const pct = Math.min(100, Math.max(0, progress));
  const done = status === 'downloaded';

  return (
    <div className="progress-cell">
      <div className="mini-bar" aria-hidden="true">
        <div className="mini-bar-fill" style={{ width: `${done ? 100 : pct}%` }} />
      </div>
      <span>{done ? '100.0%' : formatProgress(progress)}</span>
    </div>
  );
}

export default function Torrents() {
  const [filter, setFilter] = useState<Filter>('all');
  const [busyId, setBusyId] = useState<string | null>(null);

  const loader = useCallback(() => fetchTorrents(), []);
  const { data: torrents, error, loading, updatedAt, refresh } = usePolling(loader);

  const filtered = useMemo(() => {
    const items = torrents ?? [];
    return items
      .filter((t) => matchesFilter(t, filter))
      .sort((a, b) => new Date(b.added).getTime() - new Date(a.added).getTime());
  }, [torrents, filter]);

  const counts = useMemo(() => {
    const items = torrents ?? [];
    return {
      all: items.length,
      active: items.filter((t) => matchesFilter(t, 'active')).length,
      completed: items.filter((t) => matchesFilter(t, 'completed')).length,
      failed: items.filter((t) => matchesFilter(t, 'failed')).length,
    };
  }, [torrents]);

  async function handleDelete(id: string) {
    if (!confirm('Delete this torrent and its files?')) return;
    setBusyId(id);
    try {
      await deleteTorrent(id);
      await refresh();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setBusyId(null);
    }
  }

  async function handleRetry(id: string) {
    setBusyId(id);
    try {
      await retryTorrent(id);
      await refresh();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Retry failed');
    } finally {
      setBusyId(null);
    }
  }

  if (loading && !torrents) {
    return <p className="muted page-loading">Loading torrents…</p>;
  }

  return (
    <div className="torrents">
      <div className="page-toolbar">
        <div className="filter-tabs" role="tablist" aria-label="Torrent filters">
          {([
            ['all', 'All'],
            ['active', 'Active'],
            ['completed', 'Completed'],
            ['failed', 'Failed'],
          ] as const).map(([key, label]) => (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={filter === key}
              className={filter === key ? 'filter-tab active' : 'filter-tab'}
              onClick={() => setFilter(key)}
            >
              {label}
              <span className="filter-count">{counts[key]}</span>
            </button>
          ))}
        </div>

        <div className="toolbar-actions">
          {updatedAt && <span className="muted toolbar-meta">Updated {formatRelativeTime(updatedAt)}</span>}
          <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
            Refresh
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {filtered.length === 0 ? (
        <div className="empty-state card">
          <p>{torrents?.length ? 'No torrents match this filter.' : 'No torrents yet.'}</p>
          <p className="muted">Streams added from Stremio will appear here.</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Status</th>
                <th>Progress</th>
                <th>Size</th>
                <th>Speed</th>
                <th>Added</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((t) => {
                const size = t.size > 0 ? t.size : t.bytes;
                const showSpeed = t.speed > 0 && ACTIVE_STATUSES.has(t.status);

                return (
                  <tr key={t.id}>
                    <td className="name-cell" title={t.name}>
                      <span className="name-primary">{t.name}</span>
                      <span className="name-meta muted">{t.hash.slice(0, 12)}…</span>
                    </td>
                    <td>
                      <span className={`status status-${t.status}`}>{t.status.replace(/_/g, ' ')}</span>
                    </td>
                    <td>
                      <ProgressCell progress={t.progress} status={t.status} />
                    </td>
                    <td>{formatBytes(size)}</td>
                    <td>{showSpeed ? formatSpeed(t.speed) : '—'}</td>
                    <td className="muted">{formatRelativeTime(t.added)}</td>
                    <td className="actions-cell">
                      <button
                        type="button"
                        className="btn btn-danger btn-sm"
                        disabled={busyId === t.id}
                        onClick={() => handleDelete(t.id)}
                      >
                        Delete
                      </button>
                      {FAILED_STATUSES.has(t.status) && (
                        <button
                          type="button"
                          className="btn btn-secondary btn-sm"
                          disabled={busyId === t.id}
                          onClick={() => handleRetry(t.id)}
                        >
                          Retry
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
