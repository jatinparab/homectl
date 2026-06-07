import styled from '@emotion/styled';

const HeaderContainer = styled.div`
  position: sticky;
  top: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  background: ${({ theme }) => theme.colors.neutralSurface};
  min-height: 52px;
`;

const HeaderLeft = styled.div`
  display: flex;
  flex-direction: column;
  gap: 2px;
`;

const Title = styled.h1`
  font-size: 13px;
  font-weight: 600;
  color: ${({ theme }) => theme.colors.textPrimary};
  margin: 0;
`;

const Subtitle = styled.p`
  font-size: 11px;
  color: ${({ theme }) => theme.colors.textMuted};
  margin: 0;
`;

const HeaderActions = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
`;

interface PageHeaderProps {
  title: string;
  subtitle?: string;
  actions?: React.ReactNode;
  children?: React.ReactNode;
}

export function PageHeader({ title, subtitle, actions, children }: PageHeaderProps) {
  return (
    <HeaderContainer>
      <HeaderLeft>
        <Title>{title}</Title>
        {subtitle && <Subtitle>{subtitle}</Subtitle>}
      </HeaderLeft>
      <HeaderActions>
        {actions}
        {children}
      </HeaderActions>
    </HeaderContainer>
  );
}
