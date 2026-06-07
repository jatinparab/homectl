import { useMemo } from 'react';
import styled from '@emotion/styled';
import { useApps, useAgents, useUpcomingBackup, useUpcomingPrune, useSync, useRebalance, useGatewayInfo } from '../../api/hooks';
import { NodeCards } from './components/NodeCards';
import { AppsTable } from './components/AppsTable';
import { Button, Section } from '../../components/ui';

const Wrapper = styled.div`
  min-height: 100vh;
  background: ${(p) => p.theme.colors.neutralBg};
`;

const HeaderBar = styled.div`
  position: sticky;
  top: 0;
  z-index: 30;
  border-bottom: 1px solid ${(p) => p.theme.colors.border};
  background: ${(p) => p.theme.colors.neutralSurface};
  padding: 16px;
`;

const HeaderInner = styled.div`
  max-width: 72rem;
  margin: 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
`;

const HeaderTitle = styled.h1`
  font-size: ${(p) => p.theme.fontSizes.xl};
  font-weight: 700;
  color: ${(p) => p.theme.colors.textPrimary};
  margin: 0;
`;

const HeaderSub = styled.p`
  font-size: ${(p) => p.theme.fontSizes.xs};
  color: ${(p) => p.theme.colors.textMuted};
  margin: 2px 0 0;
`;

const SectionHeader = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
`;

const SectionTitle = styled.h2`
  font-size: 11px;
  font-weight: 600;
  color: ${(p) => p.theme.colors.textMuted};
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin: 0;
`;

const SectionActions = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
`;

const Main = styled.main`
  max-width: 72rem;
  margin: 0 auto;
  padding: 24px 16px;
`;

interface DashboardPageProps {
  onShowLogs: (node: string, appId: string) => void;
  onNavigate: (path: string) => void;
}

export function DashboardPage({ onShowLogs, onNavigate }: DashboardPageProps) {
  const { data: apps, isLoading } = useApps();
  const { data: agents } = useAgents();
  const { data: gatewayInfo } = useGatewayInfo();
  const { data: upcomingBackup } = useUpcomingBackup();
  const { data: upcomingPrune } = useUpcomingPrune();
  const syncMutation = useSync();
  const rebalanceMutation = useRebalance();

  const sortedApps = useMemo(() => {
    if (!apps) return [];
    return [...apps].sort((a, b) => {
      const desiredA = a.desired_state === 'running' ? 0 : 1;
      const desiredB = b.desired_state === 'running' ? 0 : 1;
      if (desiredA !== desiredB) return desiredA - desiredB;
      return a.app_id.localeCompare(b.app_id);
    });
  }, [apps]);

  const totalApps = sortedApps.length;
  const totalNodes = agents?.length ?? 0;

  return (
    <Wrapper>
      <HeaderBar>
        <HeaderInner>
          <div>
            <HeaderTitle>Dashboard</HeaderTitle>
            <HeaderSub>
              {totalApps} app{totalApps !== 1 ? 's' : ''}
              {totalNodes > 0 && ` · ${totalNodes} node${totalNodes !== 1 ? 's' : ''}`}
            </HeaderSub>
          </div>
        </HeaderInner>
      </HeaderBar>
      <Main>
        <Section>
          <SectionHeader>
            <SectionTitle>Nodes</SectionTitle>
          </SectionHeader>
          <NodeCards agents={agents} gatewayInfo={gatewayInfo} />
        </Section>
        <Section>
          <SectionHeader>
            <SectionTitle>Apps</SectionTitle>
            <SectionActions>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => syncMutation.mutate()}
                disabled={syncMutation.isPending}
                title="Git pull and sync updated configs to all agents"
              >
                {syncMutation.isPending ? 'Syncing...' : 'Sync'}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => rebalanceMutation.mutate()}
                disabled={rebalanceMutation.isPending}
                title="Rebalance all apps across nodes (may restart running apps)"
              >
                {rebalanceMutation.isPending ? 'Rebalancing...' : 'Rebalance'}
              </Button>
            </SectionActions>
          </SectionHeader>
          <AppsTable
            apps={sortedApps}
            isLoading={isLoading}
            upcomingBackup={upcomingBackup}
            upcomingPrune={upcomingPrune}
            onShowLogs={onShowLogs}
            onNavigate={onNavigate}
            agents={agents}
          />
        </Section>
      </Main>
    </Wrapper>
  );
}
