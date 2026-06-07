import styled from '@emotion/styled';

type CardVariant = 'default' | 'compact';

interface CardProps {
  variant?: CardVariant;
  children: React.ReactNode;
  className?: string;
}

const StyledCard = styled.div<{ $variant: CardVariant }>`
  background: ${({ theme }) => theme.colors.neutralSurface};
  border: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  border-radius: ${({ theme }) => theme.radii.lg};
  overflow: hidden;
  padding: ${({ $variant }) => ($variant === 'compact' ? '12px' : '16px')};
`;

export function Card({ variant = 'default', children, ...props }: CardProps) {
  return <StyledCard $variant={variant} {...props}>{children}</StyledCard>;
}
