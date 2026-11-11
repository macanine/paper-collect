import type { FormEvent, ReactNode } from 'react';
import { Button, Group, Modal, Stack } from '@mantine/core';

interface FormModalProps {
  opened: boolean;
  onClose: () => void;
  title: ReactNode;
  onSubmit: (event?: FormEvent<HTMLFormElement>) => void;
  loading?: boolean;
  okText?: string;
  cancelText?: string;
  size?: string | number;
  /** 隐藏取消按钮（仅确认）。 */
  hideCancel?: boolean;
  closeOnClickOutside?: boolean;
  children: ReactNode;
}

/** 带表单的弹窗：标题、内容区、右下角操作区的间距和对齐全站一致。 */
export function FormModal({ opened, onClose, title, onSubmit, loading, okText = '保存', cancelText = '取消', size, hideCancel, closeOnClickOutside = true, children }: FormModalProps) {
  return (
    <Modal opened={opened} onClose={onClose} title={title} size={size} closeOnClickOutside={closeOnClickOutside} closeOnEscape={closeOnClickOutside}>
      <form onSubmit={onSubmit}>
        <Stack gap="md">
          {children}
          <Group justify="flex-end" mt="xs">
            {hideCancel ? null : <Button variant="default" onClick={onClose}>{cancelText}</Button>}
            <Button type="submit" loading={loading}>{okText}</Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
