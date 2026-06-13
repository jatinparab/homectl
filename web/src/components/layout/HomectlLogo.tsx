import styled from '@emotion/styled';
import { useTheme } from '@emotion/react';
import { useId } from 'react';
import type { CSSProperties } from 'react';
import type { Theme } from '../../theme';

interface LogoMarkProps {
  size?: number;
  className?: string;
  title?: string;
}

interface LogoLockupProps {
  markSize?: number;
  className?: string;
  showTagline?: boolean;
}

const Wordmark = styled.span<{ $size: 'header' | 'large' }>`
  display: grid;
  gap: 2px;
  min-width: 0;
`;

const BrandName = styled.span<{ $size: 'header' | 'large' }>`
  color: ${({ theme }) => theme.colors.textPrimary};
  font-size: ${({ $size }) => ($size === 'large' ? '30px' : '14px')};
  font-weight: 700;
  line-height: 1;
  letter-spacing: 0;
`;

const Tagline = styled.span<{ $size: 'header' | 'large' }>`
  color: ${({ theme }) => theme.colors.textMuted};
  font-size: ${({ $size }) => ($size === 'large' ? '12px' : '10px')};
  font-weight: 500;
  line-height: 1.2;
  letter-spacing: 0;
`;

const Lockup = styled.div<{ $size: 'header' | 'large' }>`
  display: inline-flex;
  align-items: center;
  gap: ${({ $size }) => ($size === 'large' ? '14px' : '10px')};
  min-width: 0;
`;

function LogoSvg({ size = 32, className, title, style }: LogoMarkProps & { style?: CSSProperties }) {
  const theme = useTheme() as Theme;
  const generatedTitleId = useId();
  const titleId = title ? generatedTitleId : undefined;

  return (
    <svg
      viewBox="0 0 64 64"
      width={size}
      height={size}
      className={className}
      aria-hidden={title ? undefined : true}
      aria-labelledby={titleId}
      role={title ? 'img' : undefined}
      style={{ flexShrink: 0, ...style }}
    >
      {title && <title id={titleId}>{title}</title>}
      <rect width="64" height="64" rx="16" fill={theme.colors.accentPrimary} />
      <path
        d="M32 13 14 27.5v28h13.5v-16h9v16H50v-28L32 13Z"
        fill="white"
        opacity="0.96"
      />
      <path
        d="M20 30.5h24M24.5 25h23M16 55.5h48"
        stroke={theme.colors.accentMuted}
        strokeWidth="3"
        strokeLinecap="round"
        opacity="0.75"
      />
      <path
        d="M10.5 25.5 32 8l21.5 17.5"
        fill="none"
        stroke="white"
        strokeWidth="5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function HomectlFaviconLogo({ size = 32, className, title }: LogoMarkProps) {
  return <LogoSvg size={size} className={className} title={title} />;
}

export function HomectlLogo({ size = 32, className, title }: LogoMarkProps) {
  return <LogoSvg size={size} className={className} title={title} />;
}

export function HomectlLargeLogo({ size = 96, className, title = 'Homectl' }: LogoMarkProps) {
  return <LogoSvg size={size} className={className} title={title} />;
}

export function HomectlHeaderLogo({ markSize = 32, className, showTagline = false }: LogoLockupProps) {
  return (
    <Lockup $size="header" className={className}>
      <LogoSvg size={markSize} />
      <Wordmark $size="header" className="homectl-logo-wordmark">
        <BrandName $size="header">Homectl</BrandName>
        {showTagline && <Tagline $size="header">self-hosted app manager</Tagline>}
      </Wordmark>
    </Lockup>
  );
}

export function HomectlLargeLockup({ markSize = 72, className, showTagline = true }: LogoLockupProps) {
  return (
    <Lockup $size="large" className={className}>
      <LogoSvg size={markSize} />
      <Wordmark $size="large">
        <BrandName $size="large">Homectl</BrandName>
        {showTagline && <Tagline $size="large">self-hosted app manager</Tagline>}
      </Wordmark>
    </Lockup>
  );
}
