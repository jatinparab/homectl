import { useMemo, useState } from 'react';
import styled from '@emotion/styled';
import type { LogFilterParams, PublicRouteAccessLogEntry, SSHLogEntry } from '../../api/types';
import { usePublicRouteLogs, useSSHLogs } from '../../api/hooks';
import { Button } from '../../components/ui';
import { LogFilterBar } from '../../features/shared/components/LogFilterBar';
import { LogTable, type Column } from '../../features/shared/components/LogTable';

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B'; const u = ['B','KB','MB','GB','TB']; let v = bytes; let i = 0; while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; } return `${v.toFixed(i===0?0:1)} ${u[i]}`;
}
function formatDateTime(value?: string): string { if (!value) return '--'; const d = new Date(value); return Number.isNaN(d.getTime()) ? '--' : d.toLocaleString(); }
function formatShortDateTime(value?: string): string { if (!value) return '--'; const d = new Date(value); return Number.isNaN(d.getTime()) ? '--' : d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }); }
function formatDuration(seconds?: number): string { if (!seconds || seconds <= 0) return '0 ms'; if (seconds < 1) return `${Math.round(seconds * 1000)} ms`; return `${seconds.toFixed(2)} s`; }
function statusColor(status: number): string { if (status >= 500) return '#dc2626'; if (status >= 400) return '#d97706'; if (status >= 300) return '#2563eb'; if (status >= 200) return '#047857'; return '#78716c'; }
function sshBadgeStyle(s: string) { return s === 'accepted' ? {bg:'#ecfdf5',c:'#047857'} : s === 'failed' ? {bg:'#fef2f2',c:'#dc2626'} : s === 'error' ? {bg:'#fef3c7',c:'#b45309'} : s === 'disconnected' ? {bg:'#f5f5f4',c:'#78716c'} : {bg:'#f5f5f4',c:'#a8a29e'}; }
function uniqueSorted(values: string[]): string[] { return Array.from(new Set(values.filter(Boolean))).sort((a,b)=>a.localeCompare(b)); }

function isIntelligentlyExcluded(e: PublicRouteAccessLogEntry, f: { uptimePush: boolean; uptimeKumaAgent: boolean }) { return (f.uptimePush && e.host.startsWith('uptime.') && e.uri.startsWith('/api/push')) || (f.uptimeKumaAgent && e.user_agent?.startsWith('Uptime-Kuma')); }

// ── Styled ─────────────────────────────────
const Page = styled.div`min-height: 100vh; background: #faf9f7;`;
const HBar = styled.div`position: sticky; top: 0; z-index: 30; border-bottom: 1px solid ${(p)=>p.theme.colors.border}; background: ${(p)=>p.theme.colors.neutralSurface}; padding: 16px;`;
const HInner = styled.div`max-width: 72rem; margin: 0 auto; display: flex; align-items: center; justify-content: space-between;`;
const Title = styled.h1`font-size: ${(p)=>p.theme.fontSizes.sm}; font-weight: 600; color: ${(p)=>p.theme.colors.textPrimary};`;
const Sub = styled.p`font-size: 11px; color: #a8a29e; margin: 2px 0 0;`;
const Main = styled.main`max-width: 72rem; margin: 0 auto; padding: 24px 16px; display: flex; flex-direction: column; gap: 16px;`;
const Section = styled.section`background: ${(p)=>p.theme.colors.neutralSurface}; border: 1px solid ${(p)=>p.theme.colors.border}; border-radius: ${(p)=>p.theme.radii.lg}; overflow: hidden;`;
const SecHeader = styled.div`padding: 12px 16px; border-bottom: 1px solid ${(p)=>p.theme.colors.border}; display: flex; flex-direction: column; gap: 8px;`;
const SecHeaderRow = styled.div`display: flex; align-items: center; justify-content: space-between;`;
const SecTitle = styled.h2`font-size: ${(p)=>p.theme.fontSizes.xs}; font-weight: 600; color: ${(p)=>p.theme.colors.textPrimary}; text-transform: uppercase; letter-spacing: 0.1em;`;
const Count = styled.span`font-size: 11px; color: #a8a29e;`;

// ── Col renderers ──────────────────────────
const Mono = styled.span`font-family: monospace; font-size: 11px;`;
const Truncate = styled.div<{ $max?: string }>`max-width: ${(p)=>p.$max||'12rem'}; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;`;

const requestLogColumns: Column<PublicRouteAccessLogEntry>[] = [
  { header: 'Time', accessor: e => e.timestamp, render: e => <span style={{ whiteSpace:'nowrap', color:'#78716c' }}>{formatDateTime(e.timestamp)}</span>, width: 'auto' },
  { header: 'Host', accessor: e => e.host, render: e => <Mono style={{ color:'#57534e', whiteSpace:'nowrap' }}>{e.host}</Mono>, width: 'auto' },
  { header: 'IP', accessor: e => e.client_ip, render: e => <Mono style={{ color:'#57534e', whiteSpace:'nowrap' }}>{e.client_ip || '--'}</Mono>, width: 'auto' },
  { header: 'Request', accessor: e => `${e.method} ${e.uri}`, render: e => <div><div style={{ display:'flex', alignItems:'center', gap:8 }}><span style={{ fontWeight:600, color:'#57534e' }}>{e.method||'--'}</span><Truncate><Mono style={{ fontSize:11, color:'#57534e' }}>{e.uri||'/'}</Mono></Truncate></div><div style={{ fontSize:10, color:'#a8a29e', marginTop:2 }}>{e.protocol||'HTTP'} · {formatDuration(e.duration)}</div></div>, width: 'auto' },
  { header: 'Status', accessor: e => String(e.status), render: e => <div><div style={{ fontWeight:600, color: statusColor(e.status) }}>{e.status||'--'}</div><div style={{ fontSize:11, color:'#a8a29e' }}>{formatBytes(e.size)} out</div></div>, width: 'auto' },
  { header: 'Agent', accessor: e => e.user_agent||'', render: e => <Truncate $max="20rem" style={{ color:'#78716c' }} title={e.user_agent||''}>{e.user_agent||'--'}</Truncate>, width: 'auto' },
];

const sshLogColumns: Column<SSHLogEntry>[] = [
  { header: 'Time', accessor: e => e.timestamp, render: e => <span style={{ whiteSpace:'nowrap', color:'#78716c' }}>{formatShortDateTime(e.timestamp)}</span>, width: 'auto' },
  { header: 'IP', accessor: e => e.client_ip, render: e => <Mono style={{ color:'#57534e', whiteSpace:'nowrap' }}>{e.client_ip||'--'}</Mono>, width: 'auto' },
  { header: 'User', accessor: e => e.user, render: e => <Mono style={{ color:'#57534e' }}>{e.user||'--'}</Mono>, width: 'auto' },
  { header: 'Status', accessor: e => e.status, render: e => { const s = sshBadgeStyle(e.status); return <span style={{ display:'inline-block', padding:'2px 6px', borderRadius:3, fontSize:10, fontWeight:600, background:s.bg, color:s.c }}>{e.status}</span>; }, width: 'auto' },
  { header: 'Method', accessor: e => e.auth_method||'', render: e => <span style={{ color:'#78716c' }}>{e.auth_method||'--'}</span>, width: 'auto' },
  { header: 'Port', accessor: e => e.port, render: e => <Mono style={{ color:'#78716c' }}>{e.port||'--'}</Mono>, width: 'auto' },
  { header: 'Message', accessor: e => e.message, render: e => <Truncate $max="20rem" style={{ color:'#57534e' }} title={e.message}>{e.message}</Truncate>, width: 'auto' },
];

const emptyFilter: LogFilterParams = { host: undefined, ip: undefined, uri: undefined, status: undefined, search: undefined, limit: 500 };

function requestLogEntryKey(e: PublicRouteAccessLogEntry) { return [e.timestamp,e.host,e.client_ip,e.method,e.uri,e.status,e.size].join('|'); }
function sshLogEntryKey(e: SSHLogEntry, i: number) { return `${e.timestamp}-${e.client_ip}-${e.user}-${e.status}-${i}`; }

export function LogsPage() {
  const [requestDraft, setRequestDraft] = useState<LogFilterParams>({ ...emptyFilter });
  const [requestFilter, setRequestFilter] = useState<LogFilterParams>({ ...emptyFilter });
  const [intelligentFiltering, setIntelligentFiltering] = useState(true);
  const [excludeFilters, setExcludeFilters] = useState({ uptimePush: true, uptimeKumaAgent: true });
  const [sshDraft, setSSHDraft] = useState<LogFilterParams>({ ...emptyFilter });
  const [sshFilter, setSSHFilter] = useState<LogFilterParams>({ ...emptyFilter });

  const { data: rawRequestLogs, isLoading: rl } = usePublicRouteLogs(requestFilter);
  const { data: rawSSHLogs, isLoading: sl } = useSSHLogs(sshFilter);

  const allRequest = useMemo(() => rawRequestLogs ?? [], [rawRequestLogs]);
  const allSSH = useMemo(() => rawSSHLogs ?? [], [rawSSHLogs]);
  const filteredRequest = useMemo(() => intelligentFiltering ? allRequest.filter(e => !isIntelligentlyExcluded(e, excludeFilters)) : allRequest, [allRequest, intelligentFiltering, excludeFilters]);
  const hostOpts = useMemo(() => uniqueSorted(filteredRequest.map(e=>e.host)), [filteredRequest]);
  const ripOpts = useMemo(() => uniqueSorted(filteredRequest.map(e=>e.client_ip)), [filteredRequest]);
  const sipOpts = useMemo(() => uniqueSorted(allSSH.map(e=>e.client_ip)), [allSSH]);
  const statusOpts = useMemo(() => uniqueSorted(filteredRequest.map(e=>String(e.status))), [filteredRequest]);
  const sstatOpts = useMemo(() => uniqueSorted(allSSH.map(e=>e.status)), [allSSH]);

  return (
    <Page>
      <HBar><HInner><div><Title>Logs</Title><Sub>Request logs and SSH authentication logs</Sub></div></HInner></HBar>
      <Main>
        <Section>
          <SecHeader>
            <SecHeaderRow><SecTitle>Request logs</SecTitle><Count>{rl ? 'Loading...' : `${filteredRequest.length} entries`}</Count></SecHeaderRow>
            <LogFilterBar filters={requestDraft} onChange={setRequestDraft} onApply={setRequestFilter} onClear={() => { setRequestDraft({ ...emptyFilter }); setRequestFilter({ ...emptyFilter }); }} hostOptions={hostOpts} ipOptions={ripOpts} statusOptions={statusOpts} showHost showURI showStatus showSearch
              extraControls={<>
                <Button variant={intelligentFiltering ? 'primary' : 'outline'} size="sm" onClick={()=>setIntelligentFiltering(v=>!v)}>Intelligent filtering</Button>
                <Button variant={intelligentFiltering && excludeFilters.uptimePush ? 'primary' : 'outline'} size="sm" disabled={!intelligentFiltering} onClick={()=>setExcludeFilters(v=>({...v,uptimePush:!v.uptimePush}))}>uptime. /api/push</Button>
                <Button variant={intelligentFiltering && excludeFilters.uptimeKumaAgent ? 'primary' : 'outline'} size="sm" disabled={!intelligentFiltering} onClick={()=>setExcludeFilters(v=>({...v,uptimeKumaAgent:!v.uptimeKumaAgent}))}>Uptime-Kuma agent</Button>
              </>}
            />
          </SecHeader>
          <LogTable columns={requestLogColumns} entries={filteredRequest} keyFn={requestLogEntryKey} isLoading={rl} emptyMessage="No request log entries found." loadingMessage="Loading request logs..." />
        </Section>

        <Section>
          <SecHeader>
            <SecHeaderRow><SecTitle>SSH logs</SecTitle><Count>{sl ? 'Loading...' : `${allSSH.length} entries`}</Count></SecHeaderRow>
            <LogFilterBar filters={sshDraft} onChange={setSSHDraft} onApply={setSSHFilter} onClear={() => { setSSHDraft({ ...emptyFilter }); setSSHFilter({ ...emptyFilter }); }} ipOptions={sipOpts} statusOptions={sstatOpts} showStatus showSearch />
          </SecHeader>
          <LogTable columns={sshLogColumns} entries={allSSH} keyFn={sshLogEntryKey} isLoading={sl} emptyMessage="No SSH log entries found." loadingMessage="Loading SSH logs..." />
        </Section>
      </Main>
    </Page>
  );
}
