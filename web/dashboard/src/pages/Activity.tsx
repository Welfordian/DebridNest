import { useCallback, useState } from 'react';
import { fetchActivity } from '../api';
import { usePolling } from '../hooks/usePolling';
import { formatActivityLabel, formatActivitySummary } from '../lib/activityFormat';
import { formatRelativeTime } from '../lib/format';

const PAGE_SIZE = 50;

export default function Activity() {
  const [offset, setOffset] = useState(0);

  const loader = useCallback(async () => fetchActivity(PAGE_SIZE, offset), [offset]);

  const { data, error, loading, refresh } = usePolling(loader, { intervalMs: 10000 });

  const items = data ?? [];
  const hasPrev = offset > 0;
  const hasNext = items.length === PAGE_SIZE;
  const page = Math.floor(offset / PAGE_SIZE) + 1;

  if (loading && !data) {
    return <p className="muted page-loading">Loading activity…</p>;
  }

  if (error && !data) {
    return (
      <div className="page-error card">
        <p className="error">{error}</p>
        <button type="button" className="btn btn-secondary" onClick={() => refresh()}>
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="activity-page">
      <div className="page-toolbar">
        <p className="toolbar-meta muted">
          {items.length > 0
            ? `Showing ${offset + 1}–${offset + items.length}`
            : 'No events on this page'}
        </p>
        <div className="toolbar-actions">
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={!hasPrev}
            onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
          >
            Previous
          </button>
          <span className="pagination-meta muted">Page {page}</span>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={!hasNext}
            onClick={() => setOffset((o) => o + PAGE_SIZE)}
          >
            Next
          </button>
        </div>
      </div>

      {items.length === 0 ? (
        <div className="empty-state card">
          <p>No activity recorded yet.</p>
        </div>
      ) : (
        <ul className="activity-feed card">
          {items.map((event) => (
            <li key={event.id} className="activity-item">
              <div className="activity-item-main">
                <span className="activity-action">{formatActivityLabel(event)}</span>
                <span className="activity-summary">{formatActivitySummary(event)}</span>
              </div>
              <div className="activity-item-meta muted">
                {event.userName && <span>{event.userName}</span>}
                <time dateTime={event.createdAt} title={new Date(event.createdAt).toLocaleString()}>
                  {formatRelativeTime(event.createdAt)}
                </time>
              </div>
            </li>
          ))}
        </ul>
      )}

      {error && <p className="error">{error}</p>}
    </div>
  );
}
