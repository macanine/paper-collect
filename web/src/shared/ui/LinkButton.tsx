import type { ComponentProps } from 'react';
import { Button } from '@mantine/core';
import { Link } from 'react-router';

type LinkButtonProps = Omit<ComponentProps<typeof Button<'a'>>, 'component' | 'href'> & { to: string };

/** 渲染为真实链接的按钮，站内跳转不整页刷新。 */
export function LinkButton({ to, ...props }: LinkButtonProps) {
  return <Button {...props} component={Link} to={to} />;
}
