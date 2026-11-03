import type { ComponentProps } from 'react';
import { Button } from 'antd';
import { useNavigate } from 'react-router';

type LinkButtonProps = Omit<ComponentProps<typeof Button>, 'href'> & {
  to: string;
};

/** A single semantic button that performs client-side navigation. */
export function LinkButton({ to, onClick, ...props }: LinkButtonProps) {
  const navigate = useNavigate();
  return (
    <Button
      {...props}
      onClick={(event) => {
        onClick?.(event);
        if (!event.defaultPrevented) navigate(to);
      }}
    />
  );
}
