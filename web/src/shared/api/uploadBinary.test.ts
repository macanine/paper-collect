import { afterEach, describe, expect, it } from 'vitest';
import { createApiClient } from './client';
import type { SessionStore } from './session';

// 二进制接口支持显式指定 HTTP 方法。
class FakeXHR {
  static last: { method: string; url: string } | null = null;
  status = 200;
  responseText = JSON.stringify({ data: { ok: true }, request_id: 'r' });
  upload = { onprogress: null as null | ((event: ProgressEvent) => void) };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  open(method: string, url: string) {
    FakeXHR.last = { method, url };
  }
  setRequestHeader() {}
  send() {
    this.onload?.();
  }
  abort() {}
}

const original = globalThis.XMLHttpRequest;

afterEach(() => {
  if (original) globalThis.XMLHttpRequest = original;
  else delete (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest;
});

function client() {
  const session = { getAccessToken: () => 'token' } as unknown as SessionStore;
  return createApiClient({ fetch: (() => Promise.reject(new Error('unused'))) as typeof fetch, session, baseUrl: '/api/v1' });
}

describe('uploadBinary 方法', () => {
  it('默认 PUT，也可显式使用 POST', async () => {
    (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest = FakeXHR;
    const api = client();
    await api.uploadBinary('/uploads/u/files/e', new Blob(['x']));
    expect(FakeXHR.last).toEqual({ method: 'PUT', url: '/api/v1/uploads/u/files/e' });

    await api.uploadBinary('/binary', new Blob(['x']), { method: 'POST' });
    expect(FakeXHR.last).toEqual({ method: 'POST', url: '/api/v1/binary' });
  });
});
