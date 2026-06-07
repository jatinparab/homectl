import styled from '@emotion/styled';
import { keyframes } from '@emotion/react';

const spin = keyframes`
  to { transform: rotate(360deg); }
`;

const SpinnerRoot = styled.div<{ $size: number }>`
  display: inline-flex;
  align-items: center;
  justify-content: center;

  &::after {
    content: '';
    width: ${({ $size }) => $size}px;
    height: ${({ $size }) => $size}px;
    border: 2px solid ${({ theme }) => theme.colors.neutralBorder};
    border-top-color: ${({ theme }) => theme.colors.accentPrimary};
    border-radius: 50%;
    animation: ${spin} 0.6s linear infinite;
  }
`;

interface SpinnerProps {
  size?: number;
}

export function Spinner({ size = 16 }: SpinnerProps) {
  return <SpinnerRoot $size={size} />;
}
