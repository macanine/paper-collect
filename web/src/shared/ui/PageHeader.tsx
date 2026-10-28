import type { ReactNode } from 'react';

interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  extra?: ReactNode;
}

export function PageHeader({ title, description, extra }: PageHeaderProps) {
  return (
    <header className="page-heading">
      <div className="page-heading__copy">
        <div className="page-heading__text">
          <h1 className="page-heading__title">{title}</h1>
          {description ? <p className="page-heading__description">{description}</p> : null}
        </div>
      </div>
      {extra ? <div className="page-heading__extra">{extra}</div> : null}
    </header>
  );
}
