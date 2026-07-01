import type { ActivityEvent } from '../api';

function shortId(value: string): string {
  return value.length > 10 ? `${value.slice(0, 8)}…` : value;
}

function isBatchDelete(details: Record<string, unknown>): boolean {
  if (Array.isArray(details.torrentIds)) {
    return true;
  }
  return typeof details.deleted === 'number' && details.deleted > 1;
}

const ACTION_LABELS: Record<string, string> = {
  'torrent.add_magnet': 'Added magnet',
  'torrent.upload': 'Uploaded torrent',
  'torrent.delete': 'Deleted torrent',
  'torrent.batch_delete': 'Batch delete',
  'torrent.retry': 'Retried torrent',
  'torrent.purge': 'Purged torrents',
  'maintenance.cleanup': 'Maintenance cleanup',
  'settings.patch': 'Updated settings',
  'user.create': 'Created user',
  'user.delete': 'Deleted user',
  'user.rotate_token': 'Rotated token',
};

export function formatActivityLabel(event: ActivityEvent): string {
  const details = event.details ?? {};
  if (event.action === 'torrent.delete' && isBatchDelete(details)) {
    return ACTION_LABELS['torrent.batch_delete'];
  }
  return ACTION_LABELS[event.action] ?? event.action.replace(/\./g, ' · ');
}

export function formatActivitySummary(event: ActivityEvent): string {
  const d = event.details ?? {};

  switch (event.action) {
    case 'torrent.delete':
    case 'torrent.batch_delete':
      if (typeof d.deleted === 'number') {
        const failed = typeof d.failed === 'number' ? d.failed : 0;
        const label = `Removed ${d.deleted} torrent${d.deleted === 1 ? '' : 's'}`;
        return failed > 0 ? `${label} (${failed} failed)` : label;
      }
      if (typeof d.torrentId === 'string') {
        return shortId(d.torrentId);
      }
      return '—';

    case 'torrent.add_magnet':
    case 'torrent.upload':
      if (typeof d.name === 'string' && d.name) {
        return d.name;
      }
      if (typeof d.torrentId === 'string') {
        return shortId(d.torrentId);
      }
      return '—';

    case 'torrent.retry':
      if (typeof d.torrentId === 'string') {
        return shortId(d.torrentId);
      }
      return '—';

    case 'torrent.purge':
      if (typeof d.filter === 'string' && typeof d.deleted === 'number') {
        return `${d.filter} · ${d.deleted} removed`;
      }
      if (typeof d.filter === 'string') {
        return d.filter;
      }
      return '—';

    case 'maintenance.cleanup':
      if (typeof d.ageRemoved === 'number' || typeof d.quotaRemoved === 'number') {
        const parts: string[] = [];
        if (typeof d.ageRemoved === 'number' && d.ageRemoved > 0) {
          parts.push(`${d.ageRemoved} by age`);
        }
        if (typeof d.quotaRemoved === 'number' && d.quotaRemoved > 0) {
          parts.push(`${d.quotaRemoved} by quota`);
        }
        return parts.join(', ') || 'No changes';
      }
      return '—';

    case 'settings.patch':
      if (Array.isArray(d.fields) && d.fields.length > 0) {
        return d.fields.join(', ');
      }
      return 'Settings updated';

    case 'user.create':
      if (typeof d.name === 'string') {
        return d.name;
      }
      return '—';

    case 'user.delete':
    case 'user.rotate_token':
      if (typeof d.userId === 'string') {
        return shortId(d.userId);
      }
      return '—';

    default:
      return '—';
  }
}
