import styled from '@emotion/styled';

interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  selectSize?: 'sm' | 'md';
}

const StyledSelect = styled.select<{ $selectSize: 'sm' | 'md' }>`
  width: 100%;
  font-family: inherit;
  font-size: ${({ $selectSize }) => ($selectSize === 'sm' ? '11px' : '13px')};
  padding: ${({ $selectSize }) => ($selectSize === 'sm' ? '4px 8px' : '6px 10px')};
  border: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  border-radius: ${({ theme }) => theme.radii.md};
  background: ${({ theme }) => theme.colors.neutralSurface};
  color: ${({ theme }) => theme.colors.textPrimary};
  cursor: pointer;
  transition: border-color 150ms ease;
  outline: none;
  appearance: none;
  background-image: url("data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg' fill='none' viewBox='0 0 20 20'%3e%3cpath stroke='%23a8a29e' stroke-linecap='round' stroke-linejoin='round' stroke-width='1.5' d='M6 8l4 4 4-4'/%3e%3c/svg%3e");
  background-position: right 6px center;
  background-repeat: no-repeat;
  background-size: 16px;
  padding-right: 28px;

  &:focus {
    border-color: ${({ theme }) => theme.colors.accentPrimary};
    box-shadow: 0 0 0 2px ${({ theme }) => theme.colors.accentLight};
  }

  &:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
`;

export function Select({ selectSize = 'md', ...props }: SelectProps) {
  return <StyledSelect $selectSize={selectSize} {...props} />;
}
