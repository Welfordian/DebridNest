import type { Torrent, TorrentLifecycle, TorrentLifecycleGroup, TorrentLifecycleTone } from '../api';

type LifecycleSource = Pick<Torrent, 'status' | 'progress' | 'lifecycle'>;

export type TorrentFilter = 'all' | TorrentLifecycleGroup;

const ACTIVE_STATUSES = new Set([
  'magnet_conversion',
  'waiting_files_selection',
  'queued',
  'downloading',
]);

const FAILED_STATUSES = new Set(['error', 'magnet_error', 'dead']);

function normalizeStatus(status: string | undefined | null): string {
  return String(status ?? '').trim();
}

function readableStatus(status: string): string {
  const normalized = normalizeStatus(status);
  if (!normalized) return 'Unknown';
  return normalized.replace(/_/g, ' ');
}

export function statusGroup(status: string | undefined | null): TorrentLifecycleGroup {
  const normalized = normalizeStatus(status);
  if (ACTIVE_STATUSES.has(normalized)) return 'active';
  if (normalized === 'downloaded') return 'completed';
  if (FAILED_STATUSES.has(normalized)) return 'failed';
  return 'other';
}

export function statusLabel(status: string | undefined | null): string {
  switch (normalizeStatus(status)) {
    case 'magnet_conversion':
      return 'Reading metadata';
    case 'waiting_files_selection':
      return 'Selecting files';
    case 'queued':
      return 'Queued';
    case 'downloading':
      return 'Downloading';
    case 'downloaded':
      return 'Ready';
    case 'error':
      return 'Error';
    case 'magnet_error':
      return 'Magnet failed';
    case 'dead':
      return 'Dead';
    default:
      return readableStatus(status ?? '');
  }
}

export function statusDescription(status: string | undefined | null, streamable = false): string {
  if (streamable && normalizeStatus(status) !== 'downloaded') {
    return 'Stream can start while the download continues.';
  }

  switch (normalizeStatus(status)) {
    case 'magnet_conversion':
      return 'Resolving torrent metadata and file list.';
    case 'waiting_files_selection':
      return 'Metadata is ready and files are being selected.';
    case 'queued':
      return 'Files are selected and waiting for peers.';
    case 'downloading':
      return 'Selected files are downloading.';
    case 'downloaded':
      return 'Selected files are complete and ready.';
    case 'error':
      return 'The download failed.';
    case 'magnet_error':
      return 'Magnet metadata could not be resolved.';
    case 'dead':
      return 'The download is not recoverable.';
    default:
      return 'Lifecycle state is not classified.';
  }
}

export function statusTone(
  group: TorrentLifecycleGroup,
  status: string | undefined | null,
  streamable = false,
): TorrentLifecycleTone {
  if (streamable && normalizeStatus(status) !== 'downloaded') return 'streamable';
  switch (group) {
    case 'active':
      return 'active';
    case 'completed':
      return 'success';
    case 'failed':
      return 'danger';
    default:
      return 'muted';
  }
}

export function statusSortRank(status: string | undefined | null, streamable = false): number {
  if (streamable && normalizeStatus(status) !== 'downloaded') return 10;
  switch (normalizeStatus(status)) {
    case 'downloading':
      return 20;
    case 'queued':
      return 30;
    case 'waiting_files_selection':
      return 40;
    case 'magnet_conversion':
      return 50;
    case 'downloaded':
      return 60;
    case 'error':
    case 'magnet_error':
    case 'dead':
      return 90;
    default:
      return 80;
  }
}

function normalizeGroup(group: string | undefined, fallback: TorrentLifecycleGroup): TorrentLifecycleGroup {
  if (group === 'active' || group === 'completed' || group === 'failed' || group === 'other') {
    return group;
  }
  return fallback;
}

function normalizeTone(tone: string | undefined, fallback: TorrentLifecycleTone): TorrentLifecycleTone {
  if (
    tone === 'active' ||
    tone === 'success' ||
    tone === 'danger' ||
    tone === 'muted' ||
    tone === 'streamable'
  ) {
    return tone;
  }
  return fallback;
}

export function torrentLifecycle(torrent: LifecycleSource): TorrentLifecycle {
  const raw = torrent.lifecycle;
  const status = normalizeStatus(raw?.status ?? torrent.status);
  const fallbackGroup = statusGroup(status);
  const group = normalizeGroup(raw?.group, fallbackGroup);
  const streamable = Boolean(raw?.streamable ?? false);
  const completed = Boolean(raw?.completed ?? group === 'completed');
  const failed = Boolean(raw?.failed ?? group === 'failed');
  const active = Boolean(raw?.active ?? group === 'active');
  const linksVisible = Boolean(raw?.linksVisible ?? (completed || (streamable && !failed)));
  const fallbackTone = statusTone(group, status, streamable);
  const rawSortRank = raw?.sortRank;

  return {
    status,
    group,
    label: raw?.label?.trim() || statusLabel(status),
    description: raw?.description?.trim() || statusDescription(status, streamable),
    tone: normalizeTone(raw?.tone, fallbackTone),
    active,
    completed,
    failed,
    streamable,
    linksVisible,
    sortRank:
      typeof rawSortRank === 'number' && Number.isFinite(rawSortRank)
        ? rawSortRank
        : statusSortRank(status, streamable),
  };
}

export function matchesLifecycleFilter(torrent: LifecycleSource, filter: TorrentFilter): boolean {
  if (filter === 'all') return true;
  return torrentLifecycle(torrent).group === filter;
}

export function shouldShowRetry(torrent: LifecycleSource): boolean {
  return torrentLifecycle(torrent).failed;
}

export function progressPercent(torrent: LifecycleSource): number {
  const lifecycle = torrentLifecycle(torrent);
  if (lifecycle.completed) return 100;
  return Math.min(100, Math.max(0, Number(torrent.progress) || 0));
}

export function lifecycleCount(
  lifecycleCounts: Partial<Record<TorrentLifecycleGroup, number>> | undefined,
  statusCounts: Record<string, number> | undefined,
  group: TorrentLifecycleGroup,
): number {
  if (lifecycleCounts?.[group] != null) return lifecycleCounts[group] ?? 0;
  if (!statusCounts) return 0;
  return Object.entries(statusCounts).reduce(
    (sum, [status, count]) => sum + (statusGroup(status) === group ? count : 0),
    0,
  );
}
