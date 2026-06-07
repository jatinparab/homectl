import styled from '@emotion/styled';
import type { AppStatus, UpcomingBackup, UpcomingPrune, Agent } from '../../../api/types';
import { AppRow } from './AppRow';

interface AppsTableProps {
  apps: AppStatus[];
  isLoading: boolean;
  upcomingBackup?: UpcomingBackup | null;
  upcomingPrune?: UpcomingPrune | null;
  onShowLogs?: (node: string, appId: string) => void;
  onNavigate?: (path: string) => void;
  agents?: Agent[];
}

// ── Styled components ──────────────────────

const Container = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  overflow: hidden;
`;

const Table = styled.table`
  width: 100%;
  table-layout: fixed;
`;

const Th = styled.th`
  padding: 10px 12px;
  text-align: left;
  font-size: 10px;
  font-weight: 600;
  color: ${(p) => p.theme.colors.textMuted};
  text-transform: uppercase;
  letter-spacing: 0.1em;
`;

const Tr = styled.tr`
  border-bottom: 1px solid ${(p) => p.theme.colors.border};
`;

const EmptyState = styled.div`
  padding: 32px;
  text-align: center;
  font-size: ${(p) => p.theme.fontSizes.sm};
  color: ${(p) => p.theme.colors.textMuted};
`;

// Column widths as percentages
const COL_PCT = [15, 7, 13, 8, 6, 16, 13, 9, 13];

export function AppsTable({ apps, isLoading, upcomingBackup, upcomingPrune, onShowLogs, onNavigate, agents }: AppsTableProps) {
  if (isLoading) {
    return (
      <Container>
        <EmptyState>Loading...</EmptyState>
      </Container>
    );
  }

  if (apps.length === 0) {
    return (
      <Container>
        <EmptyState>No apps configured</EmptyState>
      </Container>
    );
  }

  return (
    <Container>
      <Table>
        <colgroup>
          {COL_PCT.map((w, i) => (
            <col key={`col-${i}`} style={{ width: `${w}%` }} />
          ))}
        </colgroup>
        <thead>
          <Tr>
            <Th>App</Th>
            <Th>Run</Th>
            <Th>Node</Th>
            <Th>Status</Th>
            <Th>Health</Th>
            <Th>Phase</Th>
            <Th>Backup</Th>
            <Th>Conn</Th>
            <Th></Th>
          </Tr>
        </thead>
        <tbody>
          {apps.map((app) => (
            <AppRow
              key={app.app_id}
              app={app}
              isUpcomingBackup={upcomingBackup?.app_id === app.app_id}
              upcomingBackup={upcomingBackup?.app_id === app.app_id ? upcomingBackup : undefined}
              isUpcomingPrune={upcomingPrune?.app_id === app.app_id}
              upcomingPrune={upcomingPrune?.app_id === app.app_id ? upcomingPrune : undefined}
              onShowLogs={onShowLogs}
              onNavigate={onNavigate}
              agents={agents}
            />
          ))}
        </tbody>
      </Table>
    </Container>
  );
}
