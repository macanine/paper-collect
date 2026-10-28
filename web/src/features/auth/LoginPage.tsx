import { useState } from 'react';
import { Alert, Button, Card, Form, Input } from 'antd';
import { BookOutlined, LockOutlined, UserOutlined } from '@ant-design/icons';
import { Navigate, useLocation, useNavigate } from 'react-router';
import { describeError, isApiError } from '../../shared/api/errors';
import { FullPageLoading } from '../../shared/ui/States';
import { useAuth } from './AuthContext';
import { loginRequest } from './api';

interface LoginFormValues {
  username: string;
  password: string;
}

function loginErrorText(error: unknown): string {
  if (isApiError(error) && error.status === 401) return '用户名或口令错误';
  if (isApiError(error) && error.status === 429) return '登录尝试过于频繁，请稍后再试';
  return describeError(error).title;
}

export function LoginPage() {
  const { status, signIn } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [submitting, setSubmitting] = useState(false);
  const [errorText, setErrorText] = useState<string | null>(null);

  const from = (location.state as { from?: string } | null)?.from ?? '/';

  if (status === 'authenticated') return <Navigate to={from} replace />;
  if (status === 'loading') return <FullPageLoading />;

  const onFinish = async (values: LoginFormValues) => {
    setSubmitting(true);
    setErrorText(null);
    try {
      const session = await loginRequest(values.username.trim(), values.password);
      signIn(session);
      navigate(from, { replace: true });
    } catch (error) {
      setErrorText(loginErrorText(error));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main className="login-page">
      <Card className="login-card">
        <div className="login-brand">
          <span className="login-brand__mark">
            <BookOutlined />
          </span>
          <div>
            <div className="login-brand__name">高中资料归档</div>
            <div className="login-brand__meta">Paper Collect · 管理台</div>
          </div>
        </div>
        <div className="login-heading">
          <h1>登录</h1>
          <p>使用管理员分配的账户继续</p>
        </div>
        {errorText ? <Alert className="mb-4" type="error" showIcon message={errorText} /> : null}
        <Form<LoginFormValues> layout="vertical" onFinish={onFinish} requiredMark={false} disabled={submitting}>
          <Form.Item name="username" label="用户名" rules={[{ required: true, whitespace: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} autoComplete="username" autoFocus />
          </Form.Item>
          <Form.Item name="password" label="口令" rules={[{ required: true, message: '请输入口令' }]}>
            <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={submitting}>
            登录
          </Button>
        </Form>
        <div className="login-footer">仅限授权人员访问 · 登录状态将在 15 分钟后自动续期</div>
      </Card>
    </main>
  );
}
