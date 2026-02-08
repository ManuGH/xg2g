import React from 'react';
import styles from './Button.module.css';

export type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost';
export type ButtonSize = 'md' | 'sm' | 'icon';
export type ButtonState = 'untested' | 'valid' | 'invalid';

export interface ButtonCommonProps {
  variant?: ButtonVariant;
  size?: ButtonSize;
  active?: boolean;
  state?: ButtonState;
  className?: string;
}

export type ButtonAsButtonProps = ButtonCommonProps &
  Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, keyof ButtonCommonProps | 'href'> & {
    href?: undefined;
  };

export type ButtonAsLinkProps = ButtonCommonProps &
  Omit<React.AnchorHTMLAttributes<HTMLAnchorElement>, keyof ButtonCommonProps> & {
    href: string;
  };

export type ButtonProps = ButtonAsButtonProps | ButtonAsLinkProps;

export function Button(props: ButtonAsButtonProps): React.ReactElement;
export function Button(props: ButtonAsLinkProps): React.ReactElement;
export function Button({
  variant = 'primary',
  size = 'md',
  active = false,
  state,
  className,
  ...props
}: ButtonProps) {
  const cls = [styles.button, className].filter(Boolean).join(' ');
  const shared = {
    className: cls,
    'data-variant': variant,
    'data-size': size,
    'data-active': active ? 'true' : undefined,
    'data-state': state,
  } as const;

  if ('href' in props && typeof props.href === 'string') {
    const { href, ...rest } = props;
    return <a href={href} {...shared} {...rest} />;
  }

  const { type, ...rest } = props;
  return <button type={type ?? 'button'} {...shared} {...rest} />;
}
