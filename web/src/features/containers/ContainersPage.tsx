import { useMemo } from 'react';
import styled from '@emotion/styled';
import { useContainers } from '../../api/hooks';
import { ContainerStateBadge } from '../../features/shared/components/StatusBadge';
import type { NodeContainers } from '../../api/types';

// ── Styled ─────────────────────────────────

const Page = styled.div`min-height: 100vh; background: #faf9f7;`;
const HeaderBar = styled.div`
  position: sticky;
  top: 0;
  z-index: 30;
  border-bottom: 1px solid ${(p) => p.theme.colors.border};
  background: ${(p) => p.theme.colors.neutralSurface};
  padding: 16px;
`;
const HeaderInner = styled.div`max-width: 72rem; margin: 0 auto; display: flex; align-items: center; justify-content: space-between;`;
const Title = styled.h1`font-size: ${(p) => p.theme.fontSizes.sm}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary};`;
const Subtitle = styled.p`font-size: 11px; color: #a8a29e; margin: 2px 0 0;`;
const Main = styled.main`max-width: 72rem; margin: 0 auto; padding: 24px 16px; display: flex; flex-direction: column; gap: 12px;`;
const Center = styled.div`font-size: ${(p) => p.theme.fontSizes.sm}; color: #a8a29e; padding: 32px 0; text-align: center;`;

const Card = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  overflow: hidden;
`;

const CardHeader = styled.div`
  padding: 10px 16px;
  border-bottom: 1px solid ${(p) => p.theme.colors.borderLight};
  display: flex;
  align-items: center;
  gap: 8px;
`;

const Dot = styled.span<{ $online: boolean }>`
  width: 8px; height: 8px; border-radius: 50%;
  background: ${(p) => (p.$online ? '#10b981' : '#d6d3d1')};
  ${(p) => p.$online && 'box-shadow: 0 0 6px rgba(16,185,129,0.5);'}
`;

const NodeTitle = styled.span`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const NodeSub = styled.span`font-size: 11px; color: #a8a29e;`;
const ErrorMsg = styled.span`font-size: 11px; color: #ef4444; margin-left: 8px;`;

const NoContainers = styled.div`padding: 12px 16px; font-size: 11px; color: #a8a29e;`;

const Table = styled.table`width: 100%; font-size: ${(p) => p.theme.fontSizes.xs};`;

const Th = styled.th`
  padding: 8px 12px;
  text-align: left;
  font-size: 10px;
  font-weight: 600;
  color: #a8a29e;
  text-transform: uppercase;
  letter-spacing: 0.1em;
`;

const Tr = styled.tr`
  border-bottom: 1px solid #f7f5f0;
  &:hover { background: #faf9f7; }
`;

const Td = styled.td`padding: 8px 12px;`;

const NameTd = styled(Td)`
  font-family: ${(p) => p.theme.fonts.mono};
  color: ${(p) => p.theme.colors.textPrimary};
  font-weight: 500;
`;

const ImageTd = styled(Td)`color: #a8a29e; max-width: 12rem; overflow: hidden; text-overflow: ellipsis;`;

const StatusTd = styled(Td)`color: #a8a29e;`;

const LogLink = styled.button`
  font-size: 11px;
  color: #0d9488;
  background: none;
  border: none;
  cursor: pointer;
  &:hover { color: #0f766e; text-decoration: underline; }
`;

// ── Component ──────────────────────────────

interface ContainersPageProps {
  onShowLogs: (node: string, containerId: string, containerName: string) => void;
}

export function ContainersPage({ onShowLogs }: ContainersPageProps) {
  const { data: nodeContainers, isLoading } = useContainers();

  const sortedNodes = useMemo(() => {
    if (!nodeContainers) return [];
    return [...nodeContainers].sort((a, b) => a.node.localeCompare(b.node));
  }, [nodeContainers]);

  const totalContainers = sortedNodes.reduce((n, nd) => n + nd.containers.length, 0);

  return (
    <Page>
      <HeaderBar>
        <HeaderInner>
          <div>
            <Title>Containers</Title>
            <Subtitle>
              {isLoading ? 'Loading...' : `${totalContainers} container${totalContainers !== 1 ? 's' : ''} across ${sortedNodes.length} node${sortedNodes.length !== 1 ? 's' : ''}`}
            </Subtitle>
          </div>
        </HeaderInner>
      </HeaderBar>

      <Main>
        {isLoading ? (
          <Center>Loading containers...</Center>
        ) : sortedNodes.length === 0 ? (
          <Card><NoContainers>No nodes found</NoContainers></Card>
        ) : (
          sortedNodes.map((nc) => <NodeContainersCard key={nc.node} nodeData={nc} onShowLogs={onShowLogs} />)
        )}
      </Main>
    </Page>
  );
}

function NodeContainersCard({ nodeData, onShowLogs }: { nodeData: NodeContainers; onShowLogs: (node: string, cid: string, cname: string) => void }) {
  const sorted = [...nodeData.containers].sort((a, b) => a.name.localeCompare(b.name));
  return (
    <Card>
      <CardHeader>
        <Dot $online={nodeData.online} />
        <NodeTitle>{nodeData.node}</NodeTitle>
        <NodeSub>{sorted.length} container{sorted.length !== 1 ? 's' : ''}</NodeSub>
        {nodeData.error && <ErrorMsg>{nodeData.error}</ErrorMsg>}
      </CardHeader>
      {sorted.length === 0 && !nodeData.error ? (
        <NoContainers>No containers</NoContainers>
      ) : (
        <Table>
          <thead><tr>
            <Th>Name</Th><Th>Image</Th><Th>State</Th><Th>Status</Th><Th></Th>
          </tr></thead>
          <tbody>
            {sorted.map((c) => (
              <Tr key={c.id}>
                <NameTd>{c.name}</NameTd>
                <ImageTd>{c.image}</ImageTd>
                <Td><ContainerStateBadge state={c.state} /></Td>
                <StatusTd>{c.status}</StatusTd>
                <Td><LogLink onClick={() => onShowLogs(nodeData.node, c.id, c.name)}>logs</LogLink></Td>
              </Tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  );
}
