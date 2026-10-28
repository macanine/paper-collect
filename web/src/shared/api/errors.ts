// 统一错误模型。服务端 error.code 未在 API.md 中枚举，因此优先按已知错误码映射，
// 未知码回退到 HTTP 状态码映射；服务端 message 仅作为补充说明展示（合同保证其中不含秘密）。

export interface ApiErrorInit {
  status: number;
  code: string;
  message?: string;
  serverMessage?: string;
  requestId?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly serverMessage: string | undefined;
  readonly requestId: string | undefined;

  constructor(init: ApiErrorInit) {
    super(init.message ?? statusTitle(init.status, init.code));
    this.name = 'ApiError';
    this.status = init.status;
    this.code = init.code;
    this.serverMessage = init.serverMessage;
    this.requestId = init.requestId;
  }
}

const CODE_TITLES: Record<string, string> = {
  validation_error: '提交内容不符合要求',
  bad_request: '请求格式错误',
  unauthorized: '登录已失效，请重新登录',
  invalid_credentials: '用户名或口令错误',
  forbidden: '没有执行该操作的权限',
  not_found: '请求的资源不存在',
  conflict: '当前状态不允许该操作，请刷新后重试',
  duplicate_path: '清单中存在重复路径',
  payload_too_large: '文件或请求超过大小限制',
  unsupported_media_type: '文件类型不受支持',
  unprocessable_entity: '文件内容与声明不一致',
  rate_limited: '操作过于频繁，请稍后再试',
  service_unavailable: '服务暂不可用，请稍后再试',
  internal_error: '服务器内部错误',
  network_error: '网络连接失败，请检查网络后重试',
  invalid_response: '服务响应格式不符合约定',
};

const STATUS_TITLES: Record<number, string> = {
  400: '请求格式错误',
  401: '登录已失效，请重新登录',
  403: '没有执行该操作的权限',
  404: '请求的资源不存在',
  409: '当前状态不允许该操作，请刷新后重试',
  413: '文件或请求超过大小限制',
  415: '文件类型不受支持',
  422: '文件内容与声明不一致',
  429: '操作过于频繁，请稍后再试',
  503: '服务暂不可用，请稍后再试',
};

/** 由 HTTP 状态码推导的错误码，用于服务端未返回已知 code 时的回退。 */
export function codeFromStatus(status: number): string {
  switch (status) {
    case 400:
      return 'bad_request';
    case 401:
      return 'unauthorized';
    case 403:
      return 'forbidden';
    case 404:
      return 'not_found';
    case 409:
      return 'conflict';
    case 413:
      return 'payload_too_large';
    case 415:
      return 'unsupported_media_type';
    case 422:
      return 'unprocessable_entity';
    case 429:
      return 'rate_limited';
    case 503:
      return 'service_unavailable';
    default:
      return status >= 500 ? 'internal_error' : 'http_error';
  }
}

function statusTitle(status: number, code: string): string {
  return CODE_TITLES[code] ?? STATUS_TITLES[status] ?? `请求失败（HTTP ${status}）`;
}

/** 将错误码映射为面向用户的中文标题。 */
export function titleForError(error: ApiError): string {
  return CODE_TITLES[error.code] ?? STATUS_TITLES[error.status] ?? `请求失败（HTTP ${error.status}）`;
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

export interface ErrorDescription {
  title: string;
  detail?: string;
  requestId?: string;
}

/** 将任意异常转换为可展示的标题与补充说明。 */
export function describeError(error: unknown): ErrorDescription {
  if (isApiError(error)) {
    const detail = error.serverMessage && error.serverMessage !== titleForError(error) ? error.serverMessage : undefined;
    return { title: titleForError(error), detail, requestId: error.requestId };
  }
  if (error instanceof Error && error.message) {
    return { title: error.message };
  }
  return { title: '发生未知错误' };
}

/** 生成单行错误文案，用于 message 提示。 */
export function errorMessage(error: unknown): string {
  const { title, detail } = describeError(error);
  return detail ? `${title}：${detail}` : title;
}
