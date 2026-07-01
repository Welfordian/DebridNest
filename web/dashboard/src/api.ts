const TOKEN_KEY = 'debridnest_token';

export interface Stats {
  diskUsed: number;
  diskQuota: number;
  diskQuotaGb: number;
  torrentCount: number;
  activeCount: number;
  downloadSpeed: number;
  statusCounts: Record<string, number>;
  retentionDays: number;
  publicUrl: string;
  rateLimitMbps: number;
  webdavEnabled: boolean;
  metricsEnabled: boolean;
  version?: string;
  uptimeSeconds?: number;
}

export interface Torrent {
  id: string;
  name: string;
  hash: string;
  status: string;
  progress: number;
  bytes: number;
  size: number;
  speed: number;
  seeders: number;
  added: string;
  ended?: string;
}

export interface TorrentFile {
  id: number | string;
  path: string;
  bytes: number;
  selected: boolean | number;
  downloadedBytes: number;
}

export interface TorrentDetail extends Torrent {
  files: TorrentFile[];
  links: string[];
}

export interface Config {
  publicUrl: string;
  retentionDays: number;
  diskQuotaGb: number;
  rateLimitMbps: number;
  webdavEnabled: boolean;
  metricsEnabled: boolean;
  seedAfterComplete?: boolean;
  seedRatio?: number;
  seedMinutes?: number;
  transcodeEnabled?: boolean;
  qbitUser?: string;
  linkTtlHours?: number;
  autoSelectSeconds?: number;
  minStreamMb?: number;
  streamReadaheadMb?: number;
  seekReadaheadMb?: number;
  seekPreRollMb?: number;
}

export interface SystemInfo {
  version: string;
  startedAt: string;
  uptimeSeconds: number;
  features: Record<string, boolean>;
  listen: string;
  torrentPort: number;
}

export interface RetentionResult {
  ageRemoved: number;
  quotaRemoved: number;
  diskUsed: number;
  diskQuota: number;
}

export interface AddTorrentResult {
  id: string;
}

export interface PurgeResult {
  deleted: number;
}

export type PurgeFilter = 'completed' | 'failed';

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken();
  if (!token) {
    throw new Error('Not authenticated');
  }

  const res = await fetch(path, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(init?.headers ?? {}),
    },
  });

  if (res.status === 401) {
    clearToken();
    throw new Error('Invalid token');
  }

  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      /* ignore */
    }
    throw new Error(message);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  return res.json() as Promise<T>;
}

export function fetchStats(): Promise<Stats> {
  return request<Stats>('/api/v1/stats');
}

export function fetchTorrents(): Promise<Torrent[]> {
  return request<Torrent[]>('/api/v1/torrents');
}

export function fetchTorrentDetail(id: string): Promise<TorrentDetail> {
  return request<TorrentDetail>(`/api/v1/torrents/${encodeURIComponent(id)}`);
}

export function deleteTorrent(id: string): Promise<void> {
  return request<void>(`/api/v1/torrents/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export async function deleteTorrents(ids: string[]): Promise<void> {
  await Promise.all(ids.map((id) => deleteTorrent(id)));
}

export function retryTorrent(id: string): Promise<void> {
  return request<void>(`/api/v1/torrents/${encodeURIComponent(id)}/retry`, {
    method: 'POST',
  });
}

export function fetchConfig(): Promise<Config> {
  return request<Config>('/api/v1/config');
}

export async function fetchSystem(): Promise<SystemInfo> {
  const raw = await request<Record<string, unknown>>('/api/v1/system');
  return {
    version: String(raw.version ?? ''),
    startedAt: String(raw.startedAt ?? ''),
    uptimeSeconds: Number(raw.uptime ?? raw.uptimeSeconds ?? 0),
    listen: String(raw.listen ?? ''),
    torrentPort: Number(raw.torrentPort ?? 0),
    features: {
      webdav: Boolean(raw.webdavEnabled),
      metrics: Boolean(raw.metricsEnabled),
      transcode: Boolean(raw.transcodeEnabled),
      seeding: Boolean(raw.seedAfterComplete),
      qbit: raw.qbitEnabled !== false,
    },
  };
}

export function addMagnet(magnet: string): Promise<AddTorrentResult> {
  return request<AddTorrentResult>('/api/v1/torrents/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ magnet }),
  });
}

export function uploadTorrent(file: File): Promise<AddTorrentResult> {
  const form = new FormData();
  form.append('torrent', file);
  return request<AddTorrentResult>('/api/v1/torrents/upload', {
    method: 'POST',
    body: form,
  });
}

export function runCleanup(): Promise<RetentionResult> {
  return request<RetentionResult>('/api/v1/maintenance/cleanup', {
    method: 'POST',
  });
}

export function purgeTorrents(filter: PurgeFilter): Promise<PurgeResult> {
  return request<PurgeResult>('/api/v1/torrents/purge', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ filter }),
  });
}

export function joinUrl(base: string, path: string): string {
  const trimmed = base.replace(/\/+$/, '');
  const suffix = path.startsWith('/') ? path : `/${path}`;
  return `${trimmed}${suffix}`;
}
