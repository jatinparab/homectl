import styled from '@emotion/styled';

type BadgeVariant = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent';

interface BadgeProps {
  variant?: BadgeVariant;
  children: React.ReactNode;
  className?: string;
  title?: string;
}

const variantColors: Record<BadgeVariant, { bg: string; fg: string; border: string }> = {
  neutral: { bg: '#f7f5f0', fg: '#78716c', border: '#e7e5e4' },
  success: { bg: '#ecfdf5', fg: '#065f46', border: '#a7f3d0' },
  warning: { bg: '#fffbeb', fg: '#92400e', border: '#fde68a' },
  danger: { bg: '#fef2f2', fg: '#991b1b', border: '#fecaca' },
  info: { bg: '#eff6ff', fg: '#1e40af', border: '#bfdbfe' },
  accent: { bg: '#f0fdfa', fg: '#0f766e', border: '#99f6e4' },
};

const StyledBadge = styled.span<{ $variant: BadgeVariant }>`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 10px;
  font-weight: 500;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 3px;
  white-space: nowrap;
  background: ${({ $variant }) => variantColors[$variant].bg};
  color: ${({ $variant }) => variantColors[$variant].fg};
  border: 1px solid ${({ $variant }) => variantColors[$variant].border};
`;

export function Badge({ variant = 'neutral', children, ...props }: BadgeProps) {
  return <StyledBadge $variant={variant} {...props}>{children}</StyledBadge>;
}
