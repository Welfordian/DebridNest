import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  addMagnet,
  deleteTorrent,
  deleteTorrents,
  fetchTorrents,
  retryTorrent,
  uploadTorrent,
  type Torrent,
} from '../api';
import Icon from '../components/Icon';
import { TopBarActions, TopBarMeta } from '../components/TopBar';
import { useToast } from '../components/Toast';
import { usePolling } from '../hooks/usePolling';
import { formatRelativeTime } from '../lib/format';
import {
  matchesLifecycleFilter,
  torrentLifecycle,
  type TorrentFilter,
} from '../lib/torrentLifecycle';
import AddTorrentPanel from './torrents/AddTorrentPanel';
import TorrentDetailModal from './torrents/TorrentDetailModal';
import TorrentLifecycleSummary from './torrents/TorrentLifecycleSummary';
import TorrentTable from './torrents/TorrentTable';

type TorrentCounts = Record<TorrentFilter, number>;

const FILTERS: { key: TorrentFilter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'active', label: 'Active' },
  { key: 'completed', label: 'Completed' },
  { key: 'failed', label: 'Failed' },
  { key: 'other', label: 'Other' },
];

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function compareTorrents(a: Torrent, b: Torrent): number {
  const lifecycleA = torrentLifecycle(a);
  const lifecycleB = torrentLifecycle(b);
  if (lifecycleA.sortRank !== lifecycleB.sortRank) {
    return lifecycleA.sortRank - lifecycleB.sortRank;
  }
  return new Date(b.added).getTime() - new Date(a.added).getTime();
}

function countTorrents(torrents: Torrent[]): TorrentCounts {
  return torrents.reduce<TorrentCounts>(
    (counts, torrent) => {
      counts.all += 1;
      counts[torrentLifecycle(torrent).group] += 1;
      return counts;
    },
    { all: 0, active: 0, completed: 0, failed: 0, other: 0 },
  );
}

export default function Torrents() {
  const { toast } = useToast();
  const [filter, setFilter] = useState<TorrentFilter>('all');
  const [busyId, setBusyId] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [magnet, setMagnet] = useState('');
  const [addBusy, setAddBusy] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);
  const [detailTorrent, setDetailTorrent] = useState<Torrent | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const loader = useCallback(() => fetchTorrents(), []);
  const { data: torrents, error, loading, updatedAt, refresh } = usePolling(loader);

  useEffect(() => {
    if (!torrents) return;
    const liveIds = new Set(torrents.map((torrent) => torrent.id));
    setSelected((prev) => {
      const next = new Set([...prev].filter((id) => liveIds.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [torrents]);

  const filtered = useMemo(() => {
    const items = torrents ?? [];
    return items.filter((torrent) => matchesLifecycleFilter(torrent, filter)).sort(compareTorrents);
  }, [torrents, filter]);

  const counts = useMemo(() => countTorrents(torrents ?? []), [torrents]);
  const selectedFilteredIds = useMemo(
    () => [...selected].filter((id) => filtered.some((torrent) => torrent.id === id)),
    [filtered, selected],
  );
  const allFilteredSelected =
    filtered.length > 0 && filtered.every((torrent) => selected.has(torrent.id));
  const visibleFilters = FILTERS.filter(
    (item) => item.key !== 'other' || counts.other > 0 || filter === 'other',
  );

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleSelectAll() {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allFilteredSelected) {
        filtered.forEach((torrent) => next.delete(torrent.id));
      } else {
        filtered.forEach((torrent) => next.add(torrent.id));
      }
      return next;
    });
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
      toast('Torrent deleted');
    } catch (err) {
      toast(errorMessage(err, 'Delete failed'), 'error');
    } finally {
      setBusyId(null);
    }
  }

  async function handleDeleteSelected() {
    const ids = selectedFilteredIds;
    if (ids.length === 0) return;
    if (!confirm(`Delete ${ids.length} selected torrent(s) and their files?`)) return;

    setBusyId('bulk');
    try {
      const result = await deleteTorrents(ids);
      const failed = result.failed ?? [];
      setSelected(new Set(failed));
      await refresh();

      if (result.deleted > 0) {
        toast(`Deleted ${result.deleted} torrent${result.deleted === 1 ? '' : 's'}`);
      }
      if (failed.length > 0) {
        toast(`Failed to delete ${failed.length} torrent${failed.length === 1 ? '' : 's'}`, 'error');
      }
    } catch (err) {
      toast(errorMessage(err, 'Delete failed'), 'error');
    } finally {
      setBusyId(null);
    }
  }

  async function handleRetry(id: string) {
    setBusyId(id);
    try {
      await retryTorrent(id);
      await refresh();
      toast('Retry queued');
    } catch (err) {
      toast(errorMessage(err, 'Retry failed'), 'error');
    } finally {
      setBusyId(null);
    }
  }

  async function handleAddMagnet(event: FormEvent) {
    event.preventDefault();
    const trimmed = magnet.trim();
    if (!trimmed) return;

    setAddBusy(true);
    setAddError(null);
    try {
      await addMagnet(trimmed);
      setMagnet('');
      await refresh();
      toast('Magnet added');
    } catch (err) {
      const message = errorMessage(err, 'Add magnet failed');
      setAddError(message);
      toast(message, 'error');
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
      toast('Torrent uploaded');
    } catch (err) {
      const message = errorMessage(err, 'Upload failed');
      setAddError(message);
      toast(message, 'error');
    } finally {
      setAddBusy(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  }

  if (loading && !torrents) {
    return <p className="muted page-loading">Loading torrents...</p>;
  }

  return (
    <div className="page torrents">
      <TopBarMeta>
        {updatedAt ? `updated ${formatRelativeTime(updatedAt)}` : ''}
      </TopBarMeta>
      <TopBarActions>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
          <Icon name="rotate-cw" size={14} />
          Refresh
        </button>
      </TopBarActions>

      <AddTorrentPanel
        magnet={magnet}
        busy={addBusy}
        error={addError}
        fileInputRef={fileInputRef}
        onMagnetChange={setMagnet}
        onSubmit={handleAddMagnet}
        onUpload={handleUpload}
      />

      <TorrentLifecycleSummary torrents={torrents ?? []} />

      <div className="page-toolbar torrents-toolbar">
        <div className="filter-tabs" role="tablist" aria-label="Torrent filters">
          {visibleFilters.map(({ key, label }) => (
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
          {selectedFilteredIds.length > 0 && (
            <button
              type="button"
              className="btn btn-danger btn-sm"
              disabled={busyId === 'bulk'}
              onClick={handleDeleteSelected}
            >
              Delete selected ({selectedFilteredIds.length})
            </button>
          )}
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {filtered.length === 0 ? (
        <div className="empty-state card">
          <p>{torrents?.length ? 'No torrents match this filter.' : 'No torrents yet.'}</p>
          <p className="muted">Add a magnet above or streams from Stremio will appear here.</p>
        </div>
      ) : (
        <TorrentTable
          torrents={filtered}
          selected={selected}
          busyId={busyId}
          allSelected={allFilteredSelected}
          onToggleSelect={toggleSelect}
          onToggleSelectAll={toggleSelectAll}
          onOpenDetail={setDetailTorrent}
          onDelete={handleDelete}
          onRetry={handleRetry}
        />
      )}

      {detailTorrent && (
        <TorrentDetailModal torrent={detailTorrent} onClose={() => setDetailTorrent(null)} />
      )}
    </div>
  );
}
