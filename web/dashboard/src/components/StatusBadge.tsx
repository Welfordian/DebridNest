import type { Torrent } from '../api';
import { statusGroup, statusLabel, statusTone, torrentLifecycle } from '../lib/torrentLifecycle';

export default function StatusBadge({
  status,
  torrent,
  pulse = false,
}: {
  status?: string;
  torrent?: Pick<Torrent, 'status' | 'progress' | 'lifecycle'>;
  pulse?: boolean;
}) {
  const lifecycle = torrent
    ? torrentLifecycle(torrent)
    : (() => {
        const normalized = status ?? '';
        const group = statusGroup(normalized);
        return {
          label: statusLabel(normalized),
          tone: statusTone(group, normalized),
          description: normalized,
        };
      })();

  return (
    <span
      className={`badge badge-${lifecycle.tone}${pulse ? ' badge-pulse' : ''}`}
      title={lifecycle.description}
    >
      <span className="badge-dot" />
      {lifecycle.label}
    </span>
  );
}
