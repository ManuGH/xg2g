import React from 'react';
import { Button } from './Button';
import type { ButtonSize, ButtonState, ButtonVariant } from './Button';

export interface ButtonLinkProps extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  variant?: ButtonVariant;
  size?: ButtonSize;
  active?: boolean;
  state?: ButtonState;
}

export function ButtonLink({
  variant = 'primary',
  size = 'md',
  active = false,
  state,
  ...props
}: ButtonLinkProps) {
  return (
    <Button variant={variant} size={size} active={active} state={state} {...props} />
  );
}
