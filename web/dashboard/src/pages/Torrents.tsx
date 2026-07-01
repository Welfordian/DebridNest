import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  addMagnet,
  deleteTorrent,
  deleteTorrents,
  fetchTorrentDetail,
  fetchTorrents,
  retryTorrent,
  uploadTorrent,
  type Torrent,
  type TorrentDetail,
} from '../api';
import Modal from '../components/Modal';
import { usePolling } from '../hooks/usePolling';
import {
  basename,
  formatBytes,
  formatProgress,
  formatRelativeTime,
  formatSpeed,
} from '../lib/format';
import CopyButton from '../components/CopyButton';

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

function TorrentDetailModal({
  torrent,
  onClose,
}: {
  torrent: Torrent;
  onClose: () => void;
}) {
  const [detail, setDetail] = useState<TorrentDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchTorrentDetail(torrent.id)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Load failed');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [torrent.id]);

  const size = torrent.size > 0 ? torrent.size : torrent.bytes;

  return (
    <Modal title={torrent.name} onClose={onClose}>
      <div className="detail-meta">
        <span className={`status status-${torrent.status}`}>{torrent.status.replace(/_/g, ' ')}</span>
        <span className="muted">{formatBytes(size)} · {torrent.hash}</span>
      </div>

      {loading && <p className="muted">Loading detail…</p>}
      {error && <p className="error">{error}</p>}

      {detail && (
        <>
          {detail.files.length > 0 && (
            <div className="detail-section">
              <h3>Files</h3>
              <ul className="detail-file-list">
                {detail.files.map((file) => (
                  <li key={String(file.id)}>
                    <span>{basename(file.path)}</span>
                    <span className="muted">{formatBytes(file.bytes)}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {detail.links.length > 0 && (
            <div className="detail-section">
              <h3>Links</h3>
              <ul className="detail-link-list">
                {detail.links.map((link) => (
                  <li key={link}>
                    <code className="url-value">{link}</code>
                    <CopyButton value={link} label="Copy" />
                  </li>
                ))}
              </ul>
              <p className="muted hint-text">Signed download URLs expire — prefer host links for streaming.</p>
            </div>
          )}
        </>
      )}
    </Modal>
  );
}

export default function Torrents() {
  const [filter, setFilter] = useState<Filter>('all');
  const [busyId, setBusyId] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [magnet, setMagnet] = useState('');
  const [addBusy, setAddBusy] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);
  const [detailTorrent, setDetailTorrent] = useState<Torrent | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

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

  const allFilteredSelected =
    filtered.length > 0 && filtered.every((t) => selected.has(t.id));

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleSelectAll() {
    if (allFilteredSelected) {
      setSelected((prev) => {
        const next = new Set(prev);
        filtered.forEach((t) => next.delete(t.id));
        return next;
      });
    } else {
      setSelected((prev) => {
        const next = new Set(prev);
        filtered.forEach((t) => next.add(t.id));
        return next;
      });
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this torrent and its files?')) return;
    setBusyId(id);
    try {
      await deleteTorrent(id);
      setSelected((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      await refresh();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setBusyId(null);
    }
  }

  async function handleDeleteSelected() {
    const ids = [...selected].filter((id) => filtered.some((t) => t.id === id));
    if (ids.length === 0) return;
    if (!confirm(`Delete ${ids.length} selected torrent(s) and their files?`)) return;

    setBusyId('bulk');
    try {
      await deleteTorrents(ids);
      setSelected(new Set());
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

  async function handleAddMagnet(e: FormEvent) {
    e.preventDefault();
    const trimmed = magnet.trim();
    if (!trimmed) return;

    setAddBusy(true);
    setAddError(null);
    try {
      await addMagnet(trimmed);
      setMagnet('');
      await refresh();
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Add magnet failed');
    } finally {
      setAddBusy(false);
    }
  }

  async function handleUpload(file: File) {
    setAddBusy(true);
    setAddError(null);
    try {
      await uploadTorrent(file);
      await refresh();
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Upload failed');
    } finally {
      setAddBusy(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  }

  if (loading && !torrents) {
    return <p className="muted page-loading">Loading torrents…</p>;
  }

  return (
    <div className="torrents">
      <form className="add-torrent-bar card" onSubmit={handleAddMagnet}>
        <label htmlFor="magnet-input" className="config-label">
          Add magnet
        </label>
        <div className="add-torrent-row">
          <textarea
            id="magnet-input"
            className="magnet-input"
            value={magnet}
            onChange={(e) => setMagnet(e.target.value)}
            placeholder="magnet:?xt=urn:btih:…"
            rows={2}
          />
          <div className="add-torrent-actions">
            <button type="submit" className="btn btn-primary" disabled={addBusy || !magnet.trim()}>
              {addBusy ? 'Adding…' : 'Add magnet'}
            </button>
            <label className="btn btn-secondary upload-label">
              Upload .torrent
              <input
                ref={fileInputRef}
                type="file"
                accept=".torrent,application/x-bittorrent"
                hidden
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file) handleUpload(file);
                }}
              />
            </label>
          </div>
        </div>
        {addError && <p className="error">{addError}</p>}
      </form>

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
          {selected.size > 0 && (
            <button
              type="button"
              className="btn btn-danger btn-sm"
              disabled={busyId === 'bulk'}
              onClick={handleDeleteSelected}
            >
              Delete selected ({selected.size})
            </button>
          )}
          {updatedAt && (
            <span className="muted toolbar-meta">Updated {formatRelativeTime(updatedAt)}</span>
          )}
          <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
            Refresh
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {filtered.length === 0 ? (
        <div className="empty-state card">
          <p>{torrents?.length ? 'No torrents match this filter.' : 'No torrents yet.'}</p>
          <p className="muted">Add a magnet above or streams from Stremio will appear here.</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th className="checkbox-col">
                  <input
                    type="checkbox"
                    aria-label="Select all"
                    checked={allFilteredSelected}
                    onChange={toggleSelectAll}
                  />
                </th>
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
                  <tr
                    key={t.id}
                    className="clickable-row"
                    onClick={() => setDetailTorrent(t)}
                  >
                    <td className="checkbox-col" onClick={(e) => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        aria-label={`Select ${t.name}`}
                        checked={selected.has(t.id)}
                        onChange={() => toggleSelect(t.id)}
                      />
                    </td>
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
                    <td className="actions-cell" onClick={(e) => e.stopPropagation()}>
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

      {detailTorrent && (
        <TorrentDetailModal torrent={detailTorrent} onClose={() => setDetailTorrent(null)} />
      )}
    </div>
  );
}
