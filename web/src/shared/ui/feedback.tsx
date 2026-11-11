import type { ReactNode } from 'react';
import { Button, Text, type ButtonProps } from '@mantine/core';
import { modals } from '@mantine/modals';
import { notifications } from '@mantine/notifications';

type Tone = 'green' | 'red' | 'yellow' | 'blue';

function toast(color: Tone, content: ReactNode) {
  notifications.show({ color, message: content, withBorder: true, autoClose: color === 'red' ? 6000 : 3500 });
}

/** 全局轻提示。 */
export const message = {
  success: (content: ReactNode) => toast('green', content),
  error: (content: ReactNode) => toast('red', content),
  warning: (content: ReactNode) => toast('yellow', content),
  info: (content: ReactNode) => toast('blue', content),
};

interface ConfirmOptions {
  title: ReactNode;
  content?: ReactNode;
  okText?: string;
  cancelText?: string;
  danger?: boolean;
  onOk: () => unknown;
}

/** 二次确认。onOk 返回 Promise 时确认按钮显示加载状态。 */
export const modal = {
  confirm({ title, content, okText = '确定', cancelText = '取消', danger = false, onOk }: ConfirmOptions) {
    modals.openConfirmModal({
      title,
      centered: true,
      children: content ? <Text size="sm" c="dimmed">{content}</Text> : undefined,
      labels: { confirm: okText, cancel: cancelText },
      confirmProps: { color: danger ? 'red' : undefined },
      onConfirm: () => void onOk(),
    });
  },
};

interface ConfirmButtonProps extends Omit<ButtonProps, 'onClick'> {
  confirmTitle: ReactNode;
  confirmDescription?: ReactNode;
  okText?: string;
  danger?: boolean;
  onConfirm: () => unknown;
}

/** 点击后先二次确认再执行的按钮。 */
export function ConfirmButton({ confirmTitle, confirmDescription, okText, danger, onConfirm, ...props }: ConfirmButtonProps) {
  return (
    <Button
      {...props}
      onClick={() => modal.confirm({ title: confirmTitle, content: confirmDescription, okText, danger, onOk: onConfirm })}
    />
  );
}
