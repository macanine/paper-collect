import { ApiError, codeFromStatus } from './errors';
import { type SessionStore, sessionStore } from './session';
import type { AuthSession } from './types';

export const API_BASE = '/api/v1';

export type QueryValue = string | number | boolean | null | undefined;
export type QueryParams = Record<string, QueryValue>;

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

export interface JsonRequestOptions {
  method?: HttpMethod;
  query?: QueryParams;
  body?: unknown;
  signal?: AbortSignal;
  /** 公开接口（登录、刷新）：不注入 Authorization，401 不触发刷新。 */
  public?: boolean;
}

export interface BinaryUploadProgress {
  loaded: number;
  total: number;
}

export interface BinaryUploadOptions {
  signal?: AbortSignal;
  onProgress?: (progress: BinaryUploadProgress) => void;
  /** 默认 PUT（文件上传）；其他二进制接口可指定方法。 */
  method?: 'PUT' | 'POST';
}

export interface DownloadResult {
  blob: Blob;
  filename: string | null;
}

export interface ApiClientDeps {
  fetch: typeof fetch;
  session: SessionStore;
  baseUrl?: string;
}

export interface ApiClient {
  request<T = void>(path: string, options?: JsonRequestOptions): Promise<T>;
  download(path: string, options?: { signal?: AbortSignal }): Promise<DownloadResult>;
  uploadBinary<T>(path: string, body: Blob, options?: BinaryUploadOptions): Promise<T>;
  /** 仅供测试与诊断：当前是否存在可用的 access token。 */
  hasAccessToken(): boolean;
}

/** 将查询参数序列化为 `?a=b`；undefined、null 与空字符串会被省略。 */
export function buildQuery(params?: QueryParams): string {
  if (!params) return '';
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue;
    search.set(key, String(value));
  }
  const text = search.toString();
  return text ? `?${text}` : '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError';
}

/**
 * 解析统一 envelope：成功返回 data；错误抛出 ApiError；204 返回 undefined。
 * 非 JSON 或缺失 data 的成功响应视为合同违约。
 */
export async function readEnvelope<T>(res: Response): Promise<T> {
  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = null;
    }
  }
  const requestId = isRecord(body) && typeof body.request_id === 'string' ? body.request_id : undefined;

  if (!res.ok) {
    const error = isRecord(body) && isRecord(body.error) ? body.error : null;
    const code = typeof error?.code === 'string' && error.code ? error.code : codeFromStatus(res.status);
    const serverMessage = typeof error?.message === 'string' ? error.message : undefined;
    throw new ApiError({ status: res.status, code, serverMessage, requestId });
  }

  if (res.status === 204) return undefined as T;

  if (!isRecord(body) || !('data' in body)) {
    throw new ApiError({ status: res.status, code: 'invalid_response', requestId });
  }
  return body.data as T;
}

/** 解析 Content-Disposition 中的文件名，兼容 RFC 5987 的 filename*。 */
export function parseContentDispositionFilename(header: string | null): string | null {
  if (!header) return null;
  const extended = /filename\*\s*=\s*(?:UTF-8|utf-8)''([^;]+)/i.exec(header);
  if (extended?.[1]) {
    try {
      return decodeURIComponent(extended[1].trim().replace(/^"|"$/g, ''));
    } catch {
      // 继续尝试普通 filename
    }
  }
  const plain = /filename\s*=\s*"?([^";]+)"?/i.exec(header);
  return plain?.[1]?.trim() ?? null;
}

export function createApiClient(deps: ApiClientDeps): ApiClient {
  const base = deps.baseUrl ?? API_BASE;
  const { session } = deps;
  let refreshing: Promise<string> | null = null;

  async function doFetch(url: string, init: RequestInit): Promise<Response> {
    try {
      return await deps.fetch(url, init);
    } catch (error) {
      if (isAbortError(error)) throw error;
      throw new ApiError({ status: 0, code: 'network_error' });
    }
  }

  /** 使用 refresh token 刷新会话；旧 refresh token 由服务端立即失效。 */
  async function doRefresh(): Promise<string> {
    const refreshToken = session.getRefreshToken();
    if (!refreshToken) {
      session.clear();
      throw new ApiError({ status: 401, code: 'unauthorized' });
    }
    const res = await doFetch(`${base}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });
    let data: AuthSession;
    try {
      data = await readEnvelope<AuthSession>(res);
    } catch (error) {
      // 网络错误或服务端 5xx 不代表凭据失效，保留会话；4xx 视为刷新失败并登出。
      if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
        session.clear();
      }
      throw error;
    }
    session.setSession(data);
    return data.access_token;
  }

  /**
   * 单次刷新：多个并发 401 共享同一次刷新。若 access token 已被其他请求刷新，直接复用。
   */
  async function recoverSession(usedToken: string | null): Promise<string> {
    const current = session.getAccessToken();
    if (current && current !== usedToken) return current;
    if (!refreshing) {
      refreshing = doRefresh().finally(() => {
        refreshing = null;
      });
    }
    return refreshing;
  }

  /** 发送请求；遇到 401 时刷新一次并重试一次（仅一次）。 */
  async function execute(send: (token: string | null) => Promise<Response>, options: { public?: boolean }): Promise<Response> {
    let token: string | null = null;
    if (!options.public) {
      token = session.getAccessToken();
      if (!token && session.getRefreshToken()) {
        token = await recoverSession(null);
      }
    }
    const first = await send(token);
    if (first.status !== 401 || options.public) return first;
    const fresh = await recoverSession(token);
    return send(fresh);
  }

  function authHeaders(token: string | null): Record<string, string> {
    return token ? { Authorization: `Bearer ${token}` } : {};
  }

  async function request<T = void>(path: string, options: JsonRequestOptions = {}): Promise<T> {
    const url = `${base}${path}${buildQuery(options.query)}`;
    const hasBody = options.body !== undefined;
    const res = await execute(
      (token) =>
        doFetch(url, {
          method: options.method ?? 'GET',
          headers: {
            Accept: 'application/json',
            ...(hasBody ? { 'Content-Type': 'application/json' } : {}),
            ...authHeaders(token),
          },
          body: hasBody ? JSON.stringify(options.body) : undefined,
          signal: options.signal,
        }),
      { public: options.public },
    );
    return readEnvelope<T>(res);
  }

  async function download(path: string, options: { signal?: AbortSignal } = {}): Promise<DownloadResult> {
    const res = await execute(
      (token) =>
        doFetch(`${base}${path}`, {
          method: 'GET',
          headers: { ...authHeaders(token) },
          signal: options.signal,
        }),
      {},
    );
    if (!res.ok) {
      await readEnvelope<never>(res);
    }
    const blob = await res.blob();
    return { blob, filename: parseContentDispositionFilename(res.headers.get('Content-Disposition')) };
  }

  /** 使用 XHR 发送单文件原始字节，以获得上传进度；返回 Response 以复用 envelope 解析。 */
  function sendBinaryXhr(url: string, token: string | null, body: Blob, options: BinaryUploadOptions): Promise<Response> {
    return new Promise<Response>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open(options.method ?? 'PUT', url, true);
      xhr.responseType = 'text';
      xhr.setRequestHeader('Accept', 'application/json');
      xhr.setRequestHeader('Content-Type', 'application/octet-stream');
      if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable) {
          options.onProgress?.({ loaded: event.loaded, total: event.total });
        }
      };
      xhr.onload = () => {
        resolve(
          new Response(xhr.status === 204 ? null : xhr.responseText, {
            status: xhr.status,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      };
      xhr.onerror = () => reject(new ApiError({ status: 0, code: 'network_error' }));
      xhr.onabort = () => reject(new DOMException('上传已取消', 'AbortError'));
      if (options.signal) {
        if (options.signal.aborted) {
          reject(new DOMException('上传已取消', 'AbortError'));
          return;
        }
        options.signal.addEventListener('abort', () => xhr.abort(), { once: true });
      }
      xhr.send(body);
    });
  }

  async function uploadBinary<T>(path: string, body: Blob, options: BinaryUploadOptions = {}): Promise<T> {
    const url = `${base}${path}`;
    const res = await execute((token) => sendBinaryXhr(url, token, body, options), {});
    return readEnvelope<T>(res);
  }

  return {
    request,
    download,
    uploadBinary,
    hasAccessToken: () => session.getAccessToken() !== null,
  };
}

/** 应用内使用的默认客户端，绑定浏览器 fetch 与持久化会话。 */
export const apiClient = createApiClient({
  fetch: (input, init) => fetch(input, init),
  session: sessionStore,
});

export function apiRequest<T = void>(path: string, options?: JsonRequestOptions): Promise<T> {
  return apiClient.request<T>(path, options);
}

export function apiDownload(path: string, options?: { signal?: AbortSignal }): Promise<DownloadResult> {
  return apiClient.download(path, options);
}

export function apiUploadBinary<T>(path: string, body: Blob, options?: BinaryUploadOptions): Promise<T> {
  return apiClient.uploadBinary<T>(path, body, options);
}
