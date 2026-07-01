import { useCallback, useEffect, useState } from 'react';
import {
  deleteTorrent,
  fetchTorrents,
  retryTorrent,
  type Torrent,
} from '../api';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** i;
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function formatProgress(progress: number): string {
  return `${(progress * 100).toFixed(1)}%`;
}

export default function Torrents() {
  const [torrents, setTorrents] = useState<Torrent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const data = await fetchTorrents();
      setTorrents(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load torrents');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const interval = setInterval(load, 5000);
    return () => clearInterval(interval);
  }, [load]);

  async function handleDelete(id: string) {
    if (!confirm('Delete this torrent?')) return;
    setBusyId(id);
    try {
      await deleteTorrent(id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setBusyId(null);
    }
  }

  async function handleRetry(id: string) {
    setBusyId(id);
    try {
      await retryTorrent(id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Retry failed');
    } finally {
      setBusyId(null);
    }
  }

  if (loading && torrents.length === 0) {
    return <p className="muted">Loading torrents…</p>;
  }

  return (
    <div className="torrents">
      {error && <p className="error">{error}</p>}

      {torrents.length === 0 ? (
        <p className="muted">No torrents yet.</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Status</th>
                <th>Progress</th>
                <th>Bytes</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {torrents.map((t) => (
                <tr key={t.id}>
                  <td className="name-cell" title={t.name}>
                    {t.name}
                  </td>
                  <td>
                    <span className={`status status-${t.status}`}>{t.status}</span>
                  </td>
                  <td>{formatProgress(t.progress)}</td>
                  <td>{formatBytes(t.bytes)}</td>
                  <td className="actions-cell">
                    <button
                      type="button"
                      className="btn btn-danger"
                      disabled={busyId === t.id}
                      onClick={() => handleDelete(t.id)}
                    >
                      Delete
                    </button>
                    <button
                      type="button"
                      className="btn btn-secondary"
                      disabled={busyId === t.id}
                      onClick={() => handleRetry(t.id)}
                    >
                      Retry
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
