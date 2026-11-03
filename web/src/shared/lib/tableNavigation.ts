import type { MouseEvent } from 'react';

const INTERACTIVE_SELECTOR = [
  'a',
  'button',
  'input',
  'select',
  'textarea',
  'summary',
  '[contenteditable="true"]',
  '[role="button"]',
  '[role="link"]',
  '.ant-checkbox-wrapper',
  '.ant-dropdown-trigger',
].join(',');

export function navigateTableRow(
  event: MouseEvent<HTMLElement>,
  path: string,
  navigate: (path: string) => void,
) {
  if (event.target instanceof Element && event.target.closest(INTERACTIVE_SELECTOR)) return;
  navigate(path);
}
