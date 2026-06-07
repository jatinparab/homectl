import styled from '@emotion/styled';
import type { ReactNode } from 'react';
import type { Tab } from '../../app/navigation';
import { Sidebar } from './Sidebar';

const ShellRoot = styled.div<{ $paddingBottom: number }>`
  min-height: 100vh;
  background: ${({ theme }) => theme.colors.neutralBg};
  padding-left: 80px;
  padding-bottom: ${({ $paddingBottom }) => $paddingBottom}px;

  @media (min-width: 768px) {
    padding-left: 192px;
  }
`;

const Main = styled.main`
  /* content area */
`;

interface AppShellProps {
  activeTab: Tab;
  onTabChange: (tab: Tab) => void;
  onLogout?: () => void;
  bottomPadding?: number;
  children: ReactNode;
}

export function AppShell({
  activeTab,
  onTabChange,
  onLogout,
  bottomPadding = 0,
  children,
}: AppShellProps) {
  return (
    <>
      <Sidebar activeTab={activeTab} onTabChange={onTabChange} onLogout={onLogout} />
      <ShellRoot $paddingBottom={bottomPadding}>
        <Main>{children}</Main>
      </ShellRoot>
    </>
  );
}
