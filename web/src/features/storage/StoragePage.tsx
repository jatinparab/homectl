import { useMemo } from 'react';
import styled from '@emotion/styled';
import { useStorageStatus, useTriggerStoragePrune } from '../../api/hooks';
import { Button } from '../../components/ui';

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatPercent(value?: number): string {
  if (value === undefined || Number.isNaN(value)) return '--';
  return `${value.toFixed(1)}%`;
}

function formatTime(value?: string): string {
  if (!value) return '--';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '--';
  return d.toLocaleString();
}

// ── Styled ─────────────────────────────────

const Page = styled.div`
  min-height: 100vh;
  background: #faf9f7;
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

const Title = styled.h1`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const Subtitle = styled.p`
  font-size: 11px;
  color: #a8a29e;
  margin: 2px 0 0;
`;

const Main = styled.main`
  max-width: 72rem;
  margin: 0 auto;
  padding: 24px 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
`;

const Center = styled.div`
  font-size: 11px;
  color: #a8a29e;
  padding: 32px 0;
  text-align: center;
`;

const StatGrid = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  @media (min-width: 640px) {
    grid-template-columns: repeat(4, 1fr);
  }
`;

const StatCardWrap = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  padding: 12px;
`;

const StatLabel = styled.div`
  font-size: 10px;
  color: #a8a29e;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin-bottom: 4px;
`;

const StatValue = styled.div`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const StatSub = styled.div`
  font-size: 11px;
  color: #a8a29e;
  margin-top: 2px;
`;

const StatBar = styled.div<{ $value: number }>`
  margin-top: 8px;
  height: 4px;
  width: 100%;
  background: #f5f5f4;
  border-radius: 9999px;
  overflow: hidden;
  &::after {
    content: '';
    display: block;
    height: 100%;
    width: ${(p) => `${Math.min(100, Math.max(0, p.$value))}%`};
    border-radius: 9999px;
    background: ${(p) => (p.$value >= 90 ? '#f87171' : p.$value >= 60 ? '#fbbf24' : '#14b8a6')};
  }
`;

const TableWrap = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  overflow: hidden;
`;

const Table = styled.table`
  width: 100%;
  font-size: ${(p) => p.theme.fontSizes.xs};
`;

const Th = styled.th<{ $min?: string }>`
  padding: 10px 12px;
  text-align: left;
  font-size: 10px;
  font-weight: 600;
  color: #a8a29e;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  ${(p) => p.$min && `min-width: ${p.$min};`}
`;

const Td = styled.td<{ $min?: string }>`
  padding: 10px 12px;
  ${(p) => p.$min && `min-width: ${p.$min};`}
`;

const Tr = styled.tr`
  border-bottom: 1px solid #f7f5f0;
  &:hover { background: #faf9f7; }
`;

const NodeName = styled.span`
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const OnlineStatus = styled.span<{ $online: boolean }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  font-weight: 500;
  color: ${(p) => (p.$online ? '#059669' : '#a8a29e')};
`;

const Path = styled.span`
  color: #a8a29e;
  font-family: ${(p) => p.theme.fonts.mono};
`;

const UsageLine = styled.div`
  color: #57534e;
`;

const UsageSub = styled.div`
  color: #a8a29e;
`;

const ProgressBar = styled.div<{ $value?: number; $color: (v: number) => string }>`
  margin-top: 4px;
  height: 4px;
  width: 100%;
  background: #f5f5f4;
  border-radius: 9999px;
  overflow: hidden;
  &::after {
    content: '';
    display: block;
    height: 100%;
    width: ${(p) => `${Math.min(100, Math.max(0, p.$value ?? 0))}%`};
    border-radius: 9999px;
    background: ${(p) => p.$color(p.$value ?? 0)};
  }
`;

const BusyState = styled.span`
  color: #d97706;
`;

const ErrorState = styled.span`
  color: #ef4444;
`;

const LastState = styled.span`
  color: #a8a29e;
`;

function barColor(value: number): string {
  if (value >= 90) return '#f87171';
  if (value >= 60) return '#fbbf24';
  return '#14b8a6';
}

// ── Component ────────────────────────────────

export function StoragePage() {
  const { data, isLoading } = useStorageStatus();
  const pruneMutation = useTriggerStoragePrune();

  const pruningAny = data?.any_pruning ?? false;
  const sortedNodes = useMemo(() => {
    return [...(data?.nodes ?? [])].sort((a, b) => {
      if (b.disk_free !== a.disk_free) return b.disk_free - a.disk_free;
      return a.node.localeCompare(b.node);
    });
  }, [data?.nodes]);

  function triggerPrune(node: string) {
    const scope = node === 'all' ? 'all online nodes' : node;
    if (
      !window.confirm(
        `Prune unassigned appdata on ${scope}? This deletes data directories and backup metadata for apps not assigned to those nodes.`,
      )
    )
      return;
    pruneMutation.mutate({ node });
  }

  return (
    <Page>
      <HeaderBar>
        <HeaderInner>
          <div>
            <Title>Storage</Title>
            <Subtitle>
              {isLoading ? 'Loading...' : `${sortedNodes.length} node${sortedNodes.length !== 1 ? 's' : ''}`}
            </Subtitle>
          </div>
          <Button variant="destructive" size="sm" onClick={() => triggerPrune('all')} disabled={pruneMutation.isPending || pruningAny}>
            {pruningAny ? 'Pruning...' : 'Prune all nodes'}
          </Button>
        </HeaderInner>
      </HeaderBar>

      <Main>
        {isLoading ? (
          <Center>Loading storage status...</Center>
        ) : (
          <>
            <StatGrid>
              <StatCard label="Total" value={formatBytes(data?.aggregate.disk_total)} />
              <StatCard
                label="Used"
                value={formatBytes(data?.aggregate.disk_used)}
                sub={formatPercent(data?.aggregate.disk_percent)}
                bar={data?.aggregate.disk_percent}
              />
              <StatCard label="Free" value={formatBytes(data?.aggregate.disk_free)} />
              <StatCard
                label="Reclaimable"
                value={formatBytes(data?.aggregate.reclaimable)}
                sub={formatPercent(data?.aggregate.reclaim_pct)}
                bar={data?.aggregate.reclaim_pct}
              />
            </StatGrid>

            <TableWrap>
              <Table>
                <thead>
                  <tr>
                    <Th>Node</Th>
                    <Th>Status</Th>
                    <Th>Appdata Path</Th>
                    <Th $min="12rem">Usage</Th>
                    <Th $min="10rem">Reclaimable</Th>
                    <Th>Prune</Th>
                    <Th></Th>
                  </tr>
                </thead>
                <tbody>
                  {sortedNodes.map((node) => (
                    <Tr key={node.node}>
                      <Td>
                        <NodeName>{node.node}</NodeName>
                      </Td>
                      <Td>
                        <OnlineStatus $online={node.online}>
                          {node.online ? 'online' : 'offline'}
                        </OnlineStatus>
                      </Td>
                      <Td>
                        <Path>{node.appdata_path || '—'}</Path>
                      </Td>
                      <Td $min="12rem">
                        <UsageLine>
                          {formatBytes(node.disk_used)} / {formatBytes(node.disk_total)}
                        </UsageLine>
                        <UsageSub>{formatPercent(node.disk_percent)}</UsageSub>
                        <ProgressBar $value={node.disk_percent} $color={barColor} />
                      </Td>
                      <Td $min="10rem">
                        <UsageLine>{formatBytes(node.reclaimable)}</UsageLine>
                        <UsageSub>{formatPercent(node.reclaim_pct)}</UsageSub>
                        <ProgressBar $value={node.reclaim_pct} $color={barColor} />
                      </Td>
                      <Td>
                        {node.prune.busy ? (
                          <BusyState>Pruning...</BusyState>
                        ) : node.prune.last_error ? (
                          <ErrorState>{node.prune.last_error}</ErrorState>
                        ) : (
                          <LastState>Last: {formatTime(node.prune.finished_at)}</LastState>
                        )}
                      </Td>
                      <Td>
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => triggerPrune(node.node)}
                          disabled={!node.online || node.prune.busy || pruneMutation.isPending}
                        >
                          {node.prune.busy ? 'Pruning...' : 'Prune'}
                        </Button>
                      </Td>
                    </Tr>
                  ))}
                </tbody>
              </Table>
            </TableWrap>
          </>
        )}
      </Main>
    </Page>
  );
}

function StatCard({ label, value, sub, bar }: { label: string; value: string; sub?: string; bar?: number }) {
  return (
    <StatCardWrap>
      <StatLabel>{label}</StatLabel>
      <StatValue>{value}</StatValue>
      {sub && <StatSub>{sub}</StatSub>}
      {bar !== undefined && <StatBar $value={bar} />}
    </StatCardWrap>
  );
}
