import styled from '@emotion/styled';

interface IconButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  size?: number;
  label: string;
}

const StyledIconButton = styled.button<{ $size: number }>`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: ${({ $size }) => $size}px;
  height: ${({ $size }) => $size}px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: ${({ theme }) => theme.colors.textMuted};
  cursor: pointer;
  transition: all 150ms ease;

  &:hover:not(:disabled) {
    background: ${({ theme }) => theme.colors.neutralSurfaceAlt};
    color: ${({ theme }) => theme.colors.textPrimary};
  }

  &:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
`;

export function IconButton({ size = 32, label, ...props }: IconButtonProps) {
  return <StyledIconButton $size={size} aria-label={label} title={label} {...props} />;
}
