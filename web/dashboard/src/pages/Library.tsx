import { useCallback, useMemo, useState } from 'react';
import {
  fetchStats,
  fetchTorrentDetail,
  fetchTorrents,
  joinUrl,
  type Torrent,
  type TorrentDetail,
} from '../api';
import CopyButton from '../components/CopyButton';
import Icon from '../components/Icon';
import StatusBadge from '../components/StatusBadge';
import { TopBarActions, TopBarMeta } from '../components/TopBar';
import { usePolling } from '../hooks/usePolling';
import { basename, formatBytes, formatRelativeTime } from '../lib/format';
import { torrentLifecycle } from '../lib/torrentLifecycle';

function webdavPath(hash: string, filePath: string): string {
  return `/${hash}/${basename(filePath)}`;
}

function FileRow({
  hash,
  filePath,
  bytes,
  link,
  publicUrl,
}: {
  hash: string;
  filePath: string;
  bytes: number;
  link?: string;
  publicUrl: string;
}) {
  const davPath = webdavPath(hash, filePath);
  const fullWebdav = joinUrl(publicUrl.replace(/\/+$/, ''), `/webdav${davPath}`);

  return (
    <div className="library-file-row">
      <div className="library-file-info">
        <span className="library-file-name">{basename(filePath)}</span>
        <span className="library-file-meta">{formatBytes(bytes)} · {filePath}</span>
      </div>
      <div className="library-file-actions">
        <CopyButton value={davPath} label="WebDAV path" />
        {link && <CopyButton value={link} label="Host link" />}
        <CopyButton value={fullWebdav} label="WebDAV URL" />
      </div>
    </div>
  );
}

function TorrentCard({
  torrent,
  publicUrl,
  expanded,
  onToggle,
}: {
  torrent: Torrent;
  publicUrl: string;
  expanded: boolean;
  onToggle: () => void;
}) {
  const [detail, setDetail] = useState<TorrentDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);

  async function handleToggle() {
    if (expanded) {
      onToggle();
      return;
    }
    onToggle();
    if (detail) return;

    setLoadingDetail(true);
    setDetailError(null);
    try {
      const d = await fetchTorrentDetail(torrent.id);
      setDetail(d);
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : 'Failed to load files');
    } finally {
      setLoadingDetail(false);
    }
  }

  const size = torrent.size > 0 ? torrent.size : torrent.bytes;
  const lifecycle = torrentLifecycle(torrent);
  const webdavBase = publicUrl ? joinUrl(publicUrl.replace(/\/+$/, ''), '/webdav/') : '/webdav/';

  return (
    <article className="card library-card">
      <button type="button" className="library-card-header" onClick={handleToggle}>
        <span className="library-card-title">
          <span className="name-primary">{torrent.name}</span>
          <span className="name-meta">
            {formatBytes(size)} · {lifecycle.label} · {formatRelativeTime(torrent.ended ?? torrent.added)}
          </span>
        </span>
        <span className="library-card-aside">
          <StatusBadge torrent={torrent} />
          <Icon name={expanded ? 'chevron-down' : 'chevron-right'} />
        </span>
      </button>

      {expanded && (
        <div className="library-card-body">
          {loadingDetail && <p className="muted hint-text">Loading files…</p>}
          {detailError && <p className="error hint-text">{detailError}</p>}
          {detail && detail.files.length === 0 && (
            <p className="muted hint-text">No files listed for this torrent.</p>
          )}
          {detail?.files.map((file, i) => (
            <FileRow
              key={String(file.id)}
              hash={torrent.hash}
              filePath={file.path}
              bytes={file.bytes}
              link={detail.links[i] ?? detail.links[0]}
              publicUrl={publicUrl}
            />
          ))}
          {detail && detail.files.length > 0 && (
            <p className="hint-text">
              Signed download URLs expire — use host links or WebDAV for streaming. Infuse: add{' '}
              <code>{webdavBase}</code> as a network source (Basic auth: debridnest + API token).
            </p>
          )}
        </div>
      )}
    </article>
  );
}

export default function Library() {
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const loader = useCallback(async () => {
    const [torrents, stats] = await Promise.all([fetchTorrents(), fetchStats()]);
    return { torrents, publicUrl: stats.publicUrl };
  }, []);

  const { data, error, loading, updatedAt, refresh } = usePolling(loader);

  const completed = useMemo(() => {
    const items = data?.torrents ?? [];
    return items
      .filter((t) => torrentLifecycle(t).completed)
      .sort(
        (a, b) =>
          new Date(b.ended ?? b.added).getTime() - new Date(a.ended ?? a.added).getTime(),
      );
  }, [data?.torrents]);

  if (loading && !data) {
    return <p className="muted page-loading">Loading library…</p>;
  }

  return (
    <div className="page">
      <TopBarMeta>
        {completed.length} completed torrent{completed.length === 1 ? '' : 's'}
        {updatedAt ? ` · updated ${formatRelativeTime(updatedAt)}` : ''}
      </TopBarMeta>
      <TopBarActions>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
          <Icon name="rotate-cw" size={14} />
          Refresh
        </button>
      </TopBarActions>

      <p className="section-desc">
        Stream completed downloads via WebDAV. Infuse, Kodi, and rclone can browse{' '}
        <code>/webdav/</code> on your DebridNest host.
      </p>

      {error && <p className="error">{error}</p>}

      {completed.length === 0 ? (
        <div className="empty-state card">
          <div className="empty-state-icon">
            <Icon name="library" size={20} />
          </div>
          <p>No completed torrents yet.</p>
          <p className="muted">Finished downloads appear here with WebDAV paths and stream links.</p>
        </div>
      ) : (
        <div className="library-list">
          {completed.map((t) => (
            <TorrentCard
              key={t.id}
              torrent={t}
              publicUrl={data?.publicUrl ?? ''}
              expanded={expandedId === t.id}
              onToggle={() => setExpandedId((prev) => (prev === t.id ? null : t.id))}
            />
          ))}
        </div>
      )}
    </div>
  );
}
