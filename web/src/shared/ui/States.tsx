import type { ReactNode } from 'react';
import { Alert, Button, Empty, Result, Spin } from 'antd';
import { Link } from 'react-router';
import { describeError } from '../api/errors';

export function LoadingState() {
  return (
    <div className="flex min-h-40 items-center justify-center py-10">
      <Spin />
    </div>
  );
}

export function EmptyState({ description = '暂无数据', action }: { description?: ReactNode; action?: ReactNode }) {
  return (
    <Empty description={description} image={Empty.PRESENTED_IMAGE_SIMPLE}>
      {action}
    </Empty>
  );
}

export function ErrorState({ error, onRetry, title = '加载失败' }: { error: unknown; onRetry?: () => void; title?: string }) {
  const { title: message } = describeError(error);
  return (
    <Result
      status="error"
      title={title}
      subTitle={message}
      extra={
        onRetry ? (
          <Button type="primary" onClick={onRetry}>
            重试
          </Button>
        ) : undefined
      }
    />
  );
}

export function FullPageLoading() {
  return (
    <div className="app-loading-screen">
      <Spin size="large" />
    </div>
  );
}

export function ForbiddenState() {
  return (
    <Result
      status="403"
      title="没有访问权限"
      extra={
        <Link to="/">
          <Button type="primary">返回仪表盘</Button>
        </Link>
      }
    />
  );
}

export function NotFoundPage() {
  return (
    <Result
      status="404"
      title="页面不存在"
      extra={
        <Link to="/">
          <Button type="primary">返回仪表盘</Button>
        </Link>
      }
    />
  );
}

export function InlineError({ error }: { error: unknown }) {
  return <Alert type="error" showIcon message={describeError(error).title} />;
}
