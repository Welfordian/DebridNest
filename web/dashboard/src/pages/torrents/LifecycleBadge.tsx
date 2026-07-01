import { torrentLifecycle } from '../../lib/torrentLifecycle';
import type { Torrent } from '../../api';

export default function LifecycleBadge({ torrent }: { torrent: Pick<Torrent, 'status' | 'progress' | 'lifecycle'> }) {
  const lifecycle = torrentLifecycle(torrent);

  return (
    <span className={`badge badge-${lifecycle.tone}`} title={lifecycle.description}>
      <span className="badge-dot" />
      {lifecycle.label}
    </span>
  );
}
