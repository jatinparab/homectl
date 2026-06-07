import { useTheme } from '@emotion/react';
import type { Theme } from '../../theme';

interface HomectlLogoProps {
  size?: number;
  className?: string;
}

export function HomectlLogo({ size = 32, className }: HomectlLogoProps) {
  const theme = useTheme() as Theme;
  return (
    <svg
      viewBox="0 0 32 32"
      width={size}
      height={size}
      className={className}
      aria-hidden="true"
      style={{ flexShrink: 0 }}
    >
      <rect width="32" height="32" rx="8" fill={theme.colors.accentPrimary} />
      <path d="M16 7 L7 14 L7 25 L13 25 L13 19 L19 19 L19 25 L25 25 L25 14 Z" fill="white" />
    </svg>
  );
}
