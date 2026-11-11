import { useState } from 'react';
import { Button, Tooltip } from '@mantine/core';
import { IconCheck, IconCopy } from '@tabler/icons-react';

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // 回退到 execCommand
  }
  try {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(area);
    return ok;
  } catch {
    return false;
  }
}

interface CopyButtonProps {
  value: string;
  label?: string;
  onCopied?: (ok: boolean) => void;
}

export function CopyButton({ value, label = '复制', onCopied }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  return (
    <Tooltip label={copied ? '已复制' : label}>
      <Button
        size="compact-sm"
        variant="default"
        leftSection={copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
        onClick={async () => {
          const ok = await copyToClipboard(value);
          setCopied(ok);
          onCopied?.(ok);
          if (ok) setTimeout(() => setCopied(false), 1500);
        }}
      >
        {label}
      </Button>
    </Tooltip>
  );
}
