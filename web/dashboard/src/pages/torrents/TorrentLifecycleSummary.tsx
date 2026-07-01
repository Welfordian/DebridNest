import type { Torrent } from '../../api';
import { formatSpeed } from '../../lib/format';
import { torrentLifecycle } from '../../lib/torrentLifecycle';

interface TorrentLifecycleSummaryProps {
  torrents: Torrent[];
}

export default function TorrentLifecycleSummary({ torrents }: TorrentLifecycleSummaryProps) {
  const summary = torrents.reduce(
    (acc, torrent) => {
      const lifecycle = torrentLifecycle(torrent);
      acc.total += 1;
      acc[lifecycle.group] += 1;
      if (lifecycle.streamable && !lifecycle.completed) acc.streamable += 1;
      if (lifecycle.active) acc.speed += torrent.speed || 0;
      return acc;
    },
    {
      total: 0,
      active: 0,
      completed: 0,
      failed: 0,
      other: 0,
      streamable: 0,
      speed: 0,
    },
  );

  return (
    <section className="lifecycle-summary" aria-label="Torrent lifecycle summary">
      <div className="lifecycle-summary-item">
        <span className="summary-label">Active</span>
        <strong>{summary.active}</strong>
      </div>
      <div className="lifecycle-summary-item">
        <span className="summary-label">Streamable</span>
        <strong>{summary.streamable}</strong>
      </div>
      <div className="lifecycle-summary-item">
        <span className="summary-label">Ready</span>
        <strong>{summary.completed}</strong>
      </div>
      <div className="lifecycle-summary-item">
        <span className="summary-label">Failed</span>
        <strong>{summary.failed}</strong>
      </div>
      <div className="lifecycle-summary-item lifecycle-summary-wide">
        <span className="summary-label">Download speed</span>
        <strong>{formatSpeed(summary.speed)}</strong>
      </div>
    </section>
  );
}
