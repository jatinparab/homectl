import styled from '@emotion/styled';

const EmptyContainer = styled.div`
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 32px 16px;
  text-align: center;
  color: ${({ theme }) => theme.colors.textMuted};
`;

const EmptyIcon = styled.div`
  font-size: 24px;
  margin-bottom: 8px;
  opacity: 0.5;
`;

const EmptyText = styled.p`
  font-size: 13px;
  margin: 0;
`;

interface EmptyStateProps {
  icon?: React.ReactNode;
  message?: string;
  children?: React.ReactNode;
}

export function EmptyState({ icon, message = 'No data', children }: EmptyStateProps) {
  return (
    <EmptyContainer>
      {icon && <EmptyIcon>{icon}</EmptyIcon>}
      <EmptyText>{message}</EmptyText>
      {children && <div style={{ marginTop: 12 }}>{children}</div>}
    </EmptyContainer>
  );
}
