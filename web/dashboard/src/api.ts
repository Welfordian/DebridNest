const TOKEN_KEY = 'debridnest_token';

export interface Stats {
  diskUsed: number;
  diskQuota: number;
  diskQuotaGb: number;
  torrentCount: number;
  activeCount: number;
  downloadSpeed: number;
  statusCounts: Record<string, number>;
  lifecycleCounts?: Partial<Record<TorrentLifecycleGroup, number>>;
  retentionDays: number;
  publicUrl: string;
  rateLimitMbps: number;
  webdavEnabled: boolean;
  metricsEnabled: boolean;
  version?: string;
  uptimeSeconds?: number;
}

export type TorrentLifecycleGroup = 'active' | 'completed' | 'failed' | 'other';

export type TorrentLifecycleTone = 'active' | 'success' | 'danger' | 'muted' | 'streamable';

export interface TorrentLifecycle {
  status: string;
  group: TorrentLifecycleGroup;
  label: string;
  description: string;
  tone: TorrentLifecycleTone;
  active: boolean;
  completed: boolean;
  failed: boolean;
  streamable: boolean;
  linksVisible: boolean;
  sortRank: number;
}

export interface Torrent {
  id: string;
  name: string;
  hash: string;
  status: string;
  lifecycle?: TorrentLifecycle;
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
  streamableBytes?: number;
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

export interface BatchDeleteResult {
  deleted: number;
  failed: string[];
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

export async function deleteTorrents(ids: string[]): Promise<BatchDeleteResult> {
  return request<BatchDeleteResult>('/api/v1/torrents/batch-delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
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

export interface Me {
  name: string;
  role: string;
  admin: boolean;
}

export interface Settings {
  retentionDays: number;
  diskQuotaGb: number;
  downloadRateLimitMbps: number;
  webhookDiscordUrl?: string;
  webhookNtfyTopic?: string;
  webhookGotifyUrl?: string;
  webhookGotifyToken?: string;
  notifyOnDownloadComplete?: boolean;
  notifyOnQuotaWarning?: boolean;
  s3Enabled?: boolean;
  s3Endpoint?: string;
  s3Bucket?: string;
  s3Region?: string;
  s3Prefix?: string;
  s3AccessKey?: string;
  s3SecretKey?: string;
  s3ForcePathStyle?: boolean;
  s3OffloadLocal?: boolean;
}

export interface SettingsPatch {
  retentionDays?: number;
  diskQuotaGb?: number;
  downloadRateLimitMbps?: number;
  webhookDiscordUrl?: string;
  webhookNtfyTopic?: string;
  webhookGotifyUrl?: string;
  webhookGotifyToken?: string;
  notifyOnDownloadComplete?: boolean;
  notifyOnQuotaWarning?: boolean;
  s3Enabled?: boolean;
  s3Endpoint?: string;
  s3Bucket?: string;
  s3Region?: string;
  s3Prefix?: string;
  s3AccessKey?: string;
  s3SecretKey?: string;
  s3ForcePathStyle?: boolean;
  s3OffloadLocal?: boolean;
}

export interface DashboardUser {
  id: string;
  name: string;
  role: string;
  disabled?: boolean;
  createdAt?: string;
}

export interface CreateUserRequest {
  name: string;
  role?: string;
}

export interface CreateUserResponse {
  id: string;
  name: string;
  role: string;
  token: string;
}

export interface RotateTokenResponse {
  token: string;
}

export interface ActivityEvent {
  id: number;
  userId: string;
  userName: string;
  action: string;
  details?: Record<string, unknown>;
  createdAt: string;
}

export function fetchMe(): Promise<Me> {
  return request<Me>('/api/v1/me');
}

export function fetchSettings(): Promise<Settings> {
  return request<Settings>('/api/v1/settings');
}

export function patchSettings(patch: SettingsPatch): Promise<Settings> {
  return request<Settings>('/api/v1/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  });
}

export function testS3Settings(): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>('/api/v1/settings/s3-test', { method: 'POST' });
}

export function fetchUsers(): Promise<DashboardUser[]> {
  return request<DashboardUser[]>('/api/v1/users');
}

export function createUser(body: CreateUserRequest): Promise<CreateUserResponse> {
  return request<CreateUserResponse>('/api/v1/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export function deleteUser(id: string): Promise<void> {
  return request<void>(`/api/v1/users/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export function rotateUserToken(id: string): Promise<RotateTokenResponse> {
  return request<RotateTokenResponse>(
    `/api/v1/users/${encodeURIComponent(id)}/rotate-token`,
    { method: 'POST' },
  );
}

export function fetchActivity(limit = 50, offset = 0): Promise<ActivityEvent[]> {
  const params = new URLSearchParams({
    limit: String(limit),
    offset: String(offset),
  });
  return request<ActivityEvent[]>(`/api/v1/activity?${params}`);
}

export function fetchLogs(limit = 200): Promise<string[]> {
  const params = new URLSearchParams({ limit: String(limit) });
  return request<string[]>(`/api/v1/logs?${params}`);
}
