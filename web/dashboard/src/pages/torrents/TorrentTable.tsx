import type { Torrent } from '../../api';
import Icon from '../../components/Icon';
import {
  progressPercent,
  shouldShowRetry,
  torrentLifecycle,
} from '../../lib/torrentLifecycle';
import { formatBytes, formatProgress, formatRelativeTime, formatSpeed } from '../../lib/format';
import LifecycleBadge from './LifecycleBadge';

interface TorrentTableProps {
  torrents: Torrent[];
  selected: Set<string>;
  busyId: string | null;
  allSelected: boolean;
  onToggleSelect: (id: string) => void;
  onToggleSelectAll: () => void;
  onOpenDetail: (torrent: Torrent) => void;
  onDelete: (id: string) => void;
  onRetry: (id: string) => void;
}

function ProgressCell({ torrent }: { torrent: Torrent }) {
  const pct = progressPercent(torrent);
  const lifecycle = torrentLifecycle(torrent);

  return (
    <div className="progress-cell">
      <div className="mini-bar" aria-hidden="true">
        <div className={`mini-bar-fill tone-${lifecycle.tone}`} style={{ width: `${pct}%` }} />
      </div>
      <span>{formatProgress(pct)}</span>
    </div>
  );
}

export default function TorrentTable({
  torrents,
  selected,
  busyId,
  allSelected,
  onToggleSelect,
  onToggleSelectAll,
  onOpenDetail,
  onDelete,
  onRetry,
}: TorrentTableProps) {
  return (
    <div className="table-wrap torrents-table-wrap">
      <table>
        <thead>
          <tr>
            <th className="checkbox-col">
              <input
                className="row-select"
                type="checkbox"
                aria-label="Select all"
                checked={allSelected}
                onChange={onToggleSelectAll}
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
          {torrents.map((torrent) => {
            const lifecycle = torrentLifecycle(torrent);
            const size = torrent.size > 0 ? torrent.size : torrent.bytes;
            const showSpeed = torrent.speed > 0 && lifecycle.active;

            return (
              <tr
                key={torrent.id}
                className="clickable-row"
                onClick={() => onOpenDetail(torrent)}
              >
                <td className="checkbox-col" onClick={(event) => event.stopPropagation()}>
                  <input
                    className="row-select"
                    type="checkbox"
                    aria-label={`Select ${torrent.name}`}
                    checked={selected.has(torrent.id)}
                    onChange={() => onToggleSelect(torrent.id)}
                  />
                </td>
                <td className="name-cell" title={torrent.name}>
                  <span className="name-primary">{torrent.name || 'Unnamed torrent'}</span>
                  <span className="name-meta muted">{torrent.hash ? `${torrent.hash.slice(0, 12)}...` : 'no hash'}</span>
                </td>
                <td>
                  <LifecycleBadge torrent={torrent} />
                </td>
                <td>
                  <ProgressCell torrent={torrent} />
                </td>
                <td>{formatBytes(size)}</td>
                <td>{showSpeed ? formatSpeed(torrent.speed) : '-'}</td>
                <td className="muted">{formatRelativeTime(torrent.added)}</td>
                <td className="actions-cell" onClick={(event) => event.stopPropagation()}>
                  <span className="row-actions">
                    {shouldShowRetry(torrent) && (
                      <button
                        type="button"
                        className="icon-btn"
                        title="Retry torrent"
                        aria-label={`Retry ${torrent.name}`}
                        disabled={busyId === torrent.id}
                        onClick={() => onRetry(torrent.id)}
                      >
                        <Icon name="rotate-cw" size={18} />
                      </button>
                    )}
                    <button
                      type="button"
                      className="icon-btn icon-btn-danger"
                      title="Delete torrent"
                      aria-label={`Delete ${torrent.name}`}
                      disabled={busyId === torrent.id}
                      onClick={() => onDelete(torrent.id)}
                    >
                      <Icon name="trash-2" size={18} />
                    </button>
                  </span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
