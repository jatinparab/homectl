import styled from '@emotion/styled';

const TableRoot = styled.table`
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
`;

const Thead = styled.thead`
  border-bottom: 1px solid ${({ theme }) => theme.colors.neutralBorder};
`;

const Th = styled.th`
  padding: 8px 12px;
  text-align: left;
  font-size: 10px;
  font-weight: 600;
  color: ${({ theme }) => theme.colors.textMuted};
  text-transform: uppercase;
  letter-spacing: 0.05em;
  white-space: nowrap;
`;

const Td = styled.td`
  padding: 8px 12px;
  color: ${({ theme }) => theme.colors.textPrimary};
`;

const Tr = styled.tr`
  &:not(:last-child) {
    border-bottom: 1px solid ${({ theme }) => theme.colors.neutralSurfaceAlt};
  }
  &:hover td {
    background: ${({ theme }) => theme.colors.neutralSurfaceHover};
  }
`;

const Tbody = styled.tbody``;

export const Table = Object.assign(TableRoot, { Thead, Th, Td, Tr, Tbody });
