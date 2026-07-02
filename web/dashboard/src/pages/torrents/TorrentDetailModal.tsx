import { useEffect, useState } from 'react';
import { fetchTorrentDetail, type Torrent, type TorrentDetail, type TorrentFile } from '../../api';
import CopyButton from '../../components/CopyButton';
import Modal from '../../components/Modal';
import { basename, formatBytes, formatProgress } from '../../lib/format';
import { progressPercent, torrentLifecycle } from '../../lib/torrentLifecycle';
import LifecycleBadge from './LifecycleBadge';

function fileSelected(file: TorrentFile): boolean {
  return file.selected === true || file.selected === 1;
}

function DetailStat({
  label,
  value,
  code = false,
  className = '',
}: {
  label: string;
  value: string;
  code?: boolean;
  className?: string;
}) {
  const ValueTag = code ? 'code' : 'span';

  return (
    <div className={`detail-stat ${className}`.trim()}>
      <span className="detail-stat-label">{label}</span>
      <ValueTag className={code ? 'detail-stat-value detail-stat-code' : 'detail-stat-value'} title={value}>
        {value}
      </ValueTag>
    </div>
  );
}

function FileRow({ file }: { file: TorrentFile }) {
  const downloaded = Math.max(0, file.downloadedBytes ?? 0);
  const pct = file.bytes > 0 ? Math.min(100, (downloaded / file.bytes) * 100) : 0;

  return (
    <li>
      <div className="detail-file-main">
        <span className="detail-file-name">{basename(file.path)}</span>
        <span className="muted detail-file-path">{file.path}</span>
      </div>
      <div className="detail-file-meta">
        <span className="detail-file-size">{formatBytes(file.bytes)}</span>
        <span className={fileSelected(file) ? 'pill pill-live' : 'pill pill-muted'}>
          {fileSelected(file) ? 'selected' : 'skipped'}
        </span>
        {downloaded > 0 && downloaded < file.bytes && (
          <span className="muted">{formatProgress(pct)}</span>
        )}
      </div>
    </li>
  );
}

export default function TorrentDetailModal({
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
    setDetail(null);

    fetchTorrentDetail(torrent.id)
      .then((nextDetail) => {
        if (!cancelled) setDetail(nextDetail);
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

  const current = detail ?? torrent;
  const lifecycle = torrentLifecycle(current);
  const size = current.size > 0 ? current.size : current.bytes;
  const links = detail?.links ?? [];

  return (
    <Modal title={current.name || 'Torrent detail'} onClose={onClose} wide>
      <div className="detail-stack">
        <div className="detail-meta">
          <LifecycleBadge torrent={current} />
          <span className="muted">{lifecycle.description}</span>
        </div>

        <div className="detail-summary">
          <DetailStat label="Progress" value={formatProgress(progressPercent(current))} />
          <DetailStat label="Size" value={formatBytes(size)} />
          <DetailStat label="Hash" value={current.hash || '-'} code className="detail-stat-hash" />
          <DetailStat label="Links" value={lifecycle.linksVisible ? 'Visible' : 'Hidden'} />
        </div>

        {loading && <p className="muted">Loading detail...</p>}
        {error && <p className="error">{error}</p>}

        {detail && (
          <>
            <div className="detail-section">
              <h3>Files</h3>
              {detail.files.length === 0 ? (
                <p className="muted">No files listed for this torrent.</p>
              ) : (
                <ul className="detail-file-list">
                  {detail.files.map((file) => (
                    <FileRow key={String(file.id)} file={file} />
                  ))}
                </ul>
              )}
            </div>

            <div className="detail-section">
              <h3>Links</h3>
              {lifecycle.linksVisible && links.length > 0 && (
                <>
                  <ul className="detail-link-list">
                    {links.map((link) => (
                      <li key={link}>
                        <code className="url-value">{link}</code>
                        <CopyButton value={link} label="Copy" className="btn btn-secondary btn-sm detail-copy-button" />
                      </li>
                    ))}
                  </ul>
                  <p className="muted hint-text">Signed download URLs expire. Prefer host links for streaming.</p>
                </>
              )}
              {lifecycle.linksVisible && links.length === 0 && (
                <p className="muted">Links are visible for this state, but none are available yet.</p>
              )}
              {!lifecycle.linksVisible && (
                <p className="muted">Links appear when the torrent is streamable or complete.</p>
              )}
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}
