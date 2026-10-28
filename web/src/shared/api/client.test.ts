import { describe, expect, it } from 'vitest';
import { buildQuery, readEnvelope } from './client';
import { ApiError } from './errors';

describe('buildQuery', () => {
  it('省略空值并编码特殊字符', () => {
    expect(buildQuery({ page: 1, q: '', status: undefined, tag: '数学 一轮' })).toBe('?page=1&tag=%E6%95%B0%E5%AD%A6+%E4%B8%80%E8%BD%AE');
  });

  it('无参数时返回空串', () => {
    expect(buildQuery()).toBe('');
    expect(buildQuery({})).toBe('');
  });
});

describe('readEnvelope', () => {
  it('返回 data 并忽略 request_id', async () => {
    const res = new Response(JSON.stringify({ data: { ok: true }, request_id: 'r1' }), { status: 200 });
    await expect(readEnvelope(res)).resolves.toEqual({ ok: true });
  });

  it('把错误 envelope 转成 ApiError', async () => {
    const res = new Response(JSON.stringify({ error: { code: 'validation_error', message: '标题不能为空' }, request_id: 'r2' }), {
      status: 422,
    });
    await expect(readEnvelope(res)).rejects.toMatchObject({ code: 'validation_error', status: 422, serverMessage: '标题不能为空' });
  });

  it('204 返回 undefined', async () => {
    const res = new Response(null, { status: 204 });
    await expect(readEnvelope(res)).resolves.toBeUndefined();
  });

  it('成功响应缺少 data 视为合同违约', async () => {
    const res = new Response(JSON.stringify({ request_id: 'r3' }), { status: 200 });
    await expect(readEnvelope(res)).rejects.toBeInstanceOf(ApiError);
  });
});
