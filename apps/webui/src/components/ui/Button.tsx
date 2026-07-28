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
type ButtonComponent = {
  (props: ButtonAsButtonProps & React.RefAttributes<HTMLButtonElement>): React.ReactElement;
  (props: ButtonAsLinkProps & React.RefAttributes<HTMLAnchorElement>): React.ReactElement;
};

const ButtonImpl = React.forwardRef<HTMLButtonElement | HTMLAnchorElement, ButtonProps>(function Button({
  variant = 'primary',
  size = 'md',
  active = false,
  state,
  className,
  ...props
}, ref) {
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
    return <a ref={ref as React.Ref<HTMLAnchorElement>} href={href} {...shared} {...rest} />;
  }

  const { type, ...rest } = props;
  return <button ref={ref as React.Ref<HTMLButtonElement>} type={type ?? 'button'} {...shared} {...rest} />;
});

ButtonImpl.displayName = 'Button';

export const Button = ButtonImpl as ButtonComponent;
