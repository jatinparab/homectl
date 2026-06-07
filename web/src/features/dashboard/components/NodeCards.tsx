import { useState } from 'react';
import styled from '@emotion/styled';
import { Settings } from 'lucide-react';
import type { Agent, GatewayInfo } from '../../../api/types';
import { useSetManualGateway, useClearManualGateway } from '../../../api/hooks';
import { Button, IconButton } from '../../../components/ui';
import { NodeConfigDialog } from './NodeConfigDialog';

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '--';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function pct(n?: number): string {
  if (n === undefined || Number.isNaN(n)) return '--';
  return `${n.toFixed(1)}%`;
}

interface NodeCardsProps {
  agents: Agent[] | undefined;
  gatewayInfo: GatewayInfo | undefined;
}

// ── Styled components ──────────────────────

const Grid = styled.div`
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
  margin-bottom: 24px;

  @media (min-width: 640px) {
    grid-template-columns: repeat(2, 1fr);
  }
  @media (min-width: 1024px) {
    grid-template-columns: repeat(3, 1fr);
  }
`;

const CardWrap = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  padding: 16px;
`;

const CardHeader = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
`;

const NameGroup = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
`;

const StatusDot = styled.span<{ online: boolean }>`
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: ${(p) => (p.online ? '#10b981' : '#d6d3d1')};
  ${(p) => p.online && 'box-shadow: 0 0 6px rgba(16, 185, 129, 0.5);'}
`;

const NodeName = styled.span`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const Chip = styled.span<{ variant: 'gateway' | 'manual' }>`
  font-size: 10px;
  font-weight: 500;
  padding: 2px 6px;
  border-radius: ${(p) => p.theme.radii.sm};
  text-transform: uppercase;
  letter-spacing: 0.025em;
  ${(p) =>
    p.variant === 'gateway'
      ? `color: #0f766e; background: #f0fdfa; border: 1px solid #99f6e4;`
      : `color: #1d4ed8; background: #eff6ff; border: 1px solid #bfdbfe;`}
`;

const HeaderActions = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
`;

const OnlineText = styled.span<{ online: boolean }>`
  font-size: 11px;
  font-weight: 500;
  color: ${(p) => (p.online ? '#059669' : '#a8a29e')};
`;

const IpAddress = styled.div`
  font-size: 11px;
  color: ${(p) => p.theme.colors.textMuted};
  margin-bottom: 12px;
  font-family: ${(p) => p.theme.fonts.mono};
`;

const MetricsWrap = styled.div`
  display: flex;
  flex-direction: column;
  gap: 8px;
`;

const NoMetrics = styled.div`
  font-size: 11px;
  color: ${(p) => p.theme.colors.textMuted};
`;

const Actions = styled.div`
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid ${(p) => p.theme.colors.border};
  display: flex;
  gap: 8px;
`;

// ── MetricRow ─────────────────────────────

const MetricRowRoot = styled.div``;

const MetricLabels = styled.div`
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 4px;
`;

const MetricLabel = styled.span`
  font-size: 10px;
  color: ${(p) => p.theme.colors.textMuted};
  text-transform: uppercase;
  letter-spacing: 0.05em;
`;

const MetricValue = styled.span`
  font-size: 11px;
  color: #57534e;
`;

const BarTrack = styled.div`
  height: 4px;
  width: 100%;
  background: #f5f5f4;
  border-radius: 9999px;
  overflow: hidden;
`;

const BarFill = styled.div<{ value: number }>`
  height: 100%;
  border-radius: 9999px;
  width: ${(p) => `${Math.min(100, Math.max(0, p.value))}%`};
  background: ${(p) => {
    if (p.value >= 90) return '#f87171';
    if (p.value >= 60) return '#fbbf24';
    return '#14b8a6';
  }};
`;

// ── Components ───────────────────────────

export function NodeCards({ agents, gatewayInfo }: NodeCardsProps) {
  if (!agents || agents.length === 0) return null;

  const sorted = [...agents].sort((a, b) => {
    if (a.is_gateway !== b.is_gateway) return a.is_gateway ? -1 : 1;
    return a.name.localeCompare(b.name);
  });

  return (
    <Grid>
      {sorted.map((agent) => (
        <NodeCard key={agent.name} agent={agent} gatewayInfo={gatewayInfo} />
      ))}
    </Grid>
  );
}

function NodeCard({ agent, gatewayInfo }: { agent: Agent; gatewayInfo: GatewayInfo | undefined }) {
  const setManualGateway = useSetManualGateway();
  const clearManualGateway = useClearManualGateway();
  const [settingsOpen, setSettingsOpen] = useState(false);

  const isManual = gatewayInfo?.is_manual ?? false;
  const isThisManual = isManual && gatewayInfo?.manual_node === agent.name;

  return (
    <CardWrap>
      <CardHeader>
        <NameGroup>
          <StatusDot online={agent.online} />
          <NodeName>{agent.name}</NodeName>
          {agent.is_gateway && <Chip variant="gateway">gateway</Chip>}
          {isThisManual && <Chip variant="manual">manual</Chip>}
        </NameGroup>
        <HeaderActions>
          <IconButton label={`Configure ${agent.name}`} size="1" variant="ghost" color="gray" onClick={() => setSettingsOpen(true)}>
            <Settings size={14} />
          </IconButton>
          <OnlineText online={agent.online}>{agent.online ? 'online' : 'offline'}</OnlineText>
        </HeaderActions>
      </CardHeader>

      <IpAddress>{agent.ip}</IpAddress>

      {agent.online && agent.metrics ? (
        <MetricsWrap>
          <MetricRow label="CPU" text={pct(agent.metrics.cpu_percent)} value={agent.metrics.cpu_percent ?? 0} />
          <MetricRow
            label="RAM"
            text={`${pct(agent.metrics.memory_percent)} · ${formatBytes(agent.metrics.memory_used)}/${formatBytes(agent.metrics.memory_total)}`}
            value={agent.metrics.memory_percent ?? 0}
          />
          <MetricRow
            label="Disk"
            text={`${pct(agent.metrics.disk_percent)} · ${formatBytes(agent.metrics.disk_used)}/${formatBytes(agent.metrics.disk_total)}`}
            value={agent.metrics.disk_percent ?? 0}
          />
        </MetricsWrap>
      ) : (
        <NoMetrics>no metrics available</NoMetrics>
      )}

      <Actions>
        {!isThisManual ? (
          <Button
            variant="primary"
            size="sm"
            onClick={() => setManualGateway.mutate(agent.name)}
            disabled={!agent.online || setManualGateway.isPending}
          >
            Set gateway
          </Button>
        ) : (
          <Button variant="secondary" size="sm" onClick={() => clearManualGateway.mutate()} disabled={clearManualGateway.isPending}>
            Revert to auto
          </Button>
        )}
      </Actions>
      <NodeConfigDialog node={agent.name} open={settingsOpen} onOpenChange={setSettingsOpen} />
    </CardWrap>
  );
}

function MetricRow({ label, text, value }: { label: string; text: string; value: number }) {
  return (
    <MetricRowRoot>
      <MetricLabels>
        <MetricLabel>{label}</MetricLabel>
        <MetricValue>{text}</MetricValue>
      </MetricLabels>
      <BarTrack>
        <BarFill value={value} />
      </BarTrack>
    </MetricRowRoot>
  );
}
