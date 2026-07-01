import { useCallback, useEffect, useRef } from 'react';
import { fetchLogs } from '../api';
import Icon from '../components/Icon';
import { TopBarActions, TopBarMeta } from '../components/TopBar';
import { usePolling } from '../hooks/usePolling';

function logLevel(line: string): string {
  const l = line.toLowerCase();
  if (l.includes('error') || l.includes('err ')) return 'error';
  if (l.includes('warn')) return 'warn';
  if (l.includes('debug') || l.includes('dbg')) return 'debug';
  return 'info';
}

export default function Logs() {
  const tailRef = useRef<HTMLPreElement>(null);
  const loader = useCallback(() => fetchLogs(200), []);
  const { data, error, loading, updatedAt, refresh } = usePolling(loader, { intervalMs: 3000 });

  const lines = data ?? [];

  useEffect(() => {
    const el = tailRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lines]);

  if (loading && !data) {
    return <p className="muted page-loading">Loading logs…</p>;
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
    <div className="page">
      <TopBarMeta>
        {lines.length} line{lines.length === 1 ? '' : 's'}
        {updatedAt && ` · updated ${updatedAt.toLocaleTimeString()}`}
      </TopBarMeta>
      <TopBarActions>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
          <Icon name="rotate-cw" size={14} />
          Refresh
        </button>
      </TopBarActions>

      <div className="logs-tail card">
        {lines.length === 0 ? (
          <p className="muted empty-logs">No log lines available.</p>
        ) : (
          <pre ref={tailRef} className="logs-pre">
            {lines.map((line, index) => (
              <span key={`${index}-${line.slice(0, 40)}`} className={`log-line log-${logLevel(line)}`}>
                {line}
                {'\n'}
              </span>
            ))}
          </pre>
        )}
      </div>

      {error && <p className="error">{error}</p>}
    </div>
  );
}
