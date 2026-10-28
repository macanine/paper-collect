import { QueryClient } from '@tanstack/react-query';
import { isApiError } from '../shared/api/errors';

// 全局查询配置：
// - 4xx（除 429）不自动重试，避免把权限/参数错误重复发送；网络错误与 5xx 最多重试 2 次。
// - 打开窗口不自动刷新，避免后台标签页持续请求本地服务；列表与任务页各自声明轮询。
export function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 10_000,
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => {
          if (isApiError(error) && error.status >= 400 && error.status < 500 && error.status !== 429) {
            return false;
          }
          return failureCount < 2;
        },
      },
      mutations: {
        retry: false,
      },
    },
  });
}

export const queryClient = createAppQueryClient();
