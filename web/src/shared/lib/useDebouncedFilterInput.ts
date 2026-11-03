import { useEffect, useState } from 'react';

/**
 * Keeps a text filter responsive while committing only the trimmed value to
 * the URL and API query. External URL changes, such as Back or "clear", are
 * reflected back into the input.
 */
export function useDebouncedFilterInput(
  value: string,
  commit: (value: string) => void,
  delay = 300,
) {
  const [input, setInput] = useState(value);

  useEffect(() => {
    setInput(value);
  }, [value]);

  useEffect(() => {
    const trimmed = input.trim();
    if (trimmed === value) return;
    const timer = window.setTimeout(() => commit(trimmed), delay);
    return () => window.clearTimeout(timer);
  }, [commit, delay, input, value]);

  return [input, setInput] as const;
}
