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

export interface Config {
  publicUrl: string;
  retentionDays: number;
  diskQuotaGb: number;
  rateLimitMbps: number;
  webdavEnabled: boolean;
  metricsEnabled: boolean;
}

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

export function deleteTorrent(id: string): Promise<void> {
  return request<void>(`/api/v1/torrents/${encodeURIComponent(id)}`, {
    method: 'DELETE',
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
