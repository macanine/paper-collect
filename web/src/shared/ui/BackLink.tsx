import { ArrowLeftOutlined } from '@ant-design/icons';
import { Link } from 'react-router';

export function BackLink({ to, children }: { to: string; children: string }) {
  return (
    <Link to={to} className="detail-back-link">
      <ArrowLeftOutlined />
      {children}
    </Link>
  );
}
