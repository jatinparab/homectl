import styled from '@emotion/styled';

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  inputSize?: 'sm' | 'md';
}

const StyledInput = styled.input<{ $inputSize: 'sm' | 'md' }>`
  width: 100%;
  font-family: inherit;
  font-size: ${({ $inputSize }) => ($inputSize === 'sm' ? '11px' : '13px')};
  padding: ${({ $inputSize }) => ($inputSize === 'sm' ? '4px 8px' : '6px 10px')};
  border: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  border-radius: ${({ theme }) => theme.radii.md};
  background: ${({ theme }) => theme.colors.neutralSurface};
  color: ${({ theme }) => theme.colors.textPrimary};
  transition: border-color 150ms ease;
  outline: none;

  &::placeholder {
    color: ${({ theme }) => theme.colors.textMuted};
  }

  &:focus {
    border-color: ${({ theme }) => theme.colors.accentPrimary};
    box-shadow: 0 0 0 2px ${({ theme }) => theme.colors.accentLight};
  }

  &:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
`;

export function Input({ inputSize = 'md', ...props }: InputProps) {
  return <StyledInput $inputSize={inputSize} {...props} />;
}
