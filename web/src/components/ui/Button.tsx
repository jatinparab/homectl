import styled from '@emotion/styled';
import { css } from '@emotion/react';
import type { Theme } from '../../theme';

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'destructive';
type ButtonSize = 'sm' | 'md';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
}

const variantStyles = (theme: Theme): Record<ButtonVariant, ReturnType<typeof css>> => ({
  primary: css`
    background: ${theme.colors.accentPrimary};
    color: white;
    border: 1px solid ${theme.colors.accentPrimary};
    &:hover:not(:disabled) {
      background: ${theme.colors.accentHover};
      border-color: ${theme.colors.accentHover};
    }
  `,
  secondary: css`
    background: ${theme.colors.neutralSurface};
    color: ${theme.colors.textSecondary};
    border: 1px solid ${theme.colors.neutralBorder};
    &:hover:not(:disabled) {
      border-color: ${theme.colors.neutralBorderStrong};
      color: ${theme.colors.textPrimary};
    }
  `,
  ghost: css`
    background: transparent;
    color: ${theme.colors.textMuted};
    border: 1px solid transparent;
    &:hover:not(:disabled) {
      background: ${theme.colors.neutralSurfaceAlt};
      color: ${theme.colors.textPrimary};
    }
  `,
  destructive: css`
    background: ${theme.colors.danger};
    color: white;
    border: 1px solid ${theme.colors.danger};
    &:hover:not(:disabled) {
      background: #b91c1c;
      border-color: #b91c1c;
    }
  `,
});

const sizeStyles: Record<ButtonSize, ReturnType<typeof css>> = {
  sm: css`
    padding: 3px 8px;
    font-size: 11px;
    border-radius: 3px;
  `,
  md: css`
    padding: 5px 12px;
    font-size: 13px;
    border-radius: 6px;
  `,
};

const StyledButton = styled.button<{ $variant: ButtonVariant; $size: ButtonSize }>`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-weight: 500;
  line-height: 1.4;
  cursor: pointer;
  transition: all 150ms ease;
  white-space: nowrap;
  ${({ $variant, theme }) => variantStyles(theme)[$variant]}
  ${({ $size }) => sizeStyles[$size]}

  &:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
`;

export function Button({ variant = 'secondary', size = 'md', ...props }: ButtonProps) {
  return <StyledButton $variant={variant} $size={size} {...props} />;
}
