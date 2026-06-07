import { useEffect, useMemo, useState } from 'react';
import type { FormEvent } from 'react';
import styled from '@emotion/styled';
import { useAddCrowdSecDecision, useCrowdSecDecisions, useCrowdSecHub, useCrowdSecLogs, useCrowdSecMetrics, useCrowdSecStatus, useCrowdSecVersion, useDeleteCrowdSecDecision, useInstallCrowdSecHubItem, useRemoveCrowdSecHubItem, useUpdateCrowdSec } from '../../api/hooks';
import type { CrowdSecDecision, CrowdSecHubItem, CrowdSecLogEntry } from '../../api/types';
import { Button } from '../../components/ui';

function formatDateTime(v?: string) { if (!v) return '--'; const d = new Date(v); return Number.isNaN(d.getTime()) ? v : d.toLocaleString(); }
function formatDuration(v?: string) { if (!v) return '--'; return v.replace(/m\d+s$/, 'm').replace(/h\d+m$/, 'h'); }
function countryFlag(code?: string) { if (!code || code.length !== 2) return ''; return String.fromCodePoint(...[...code.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65)); }
function decisionKey(d: CrowdSecDecision) { return `${d.id ?? ''}-${d.scope}-${d.value}-${d.type}`; }
function logKey(e: CrowdSecLogEntry, i: number) { return `${e.timestamp ?? ''}-${e.source}-${e.level ?? ''}-${e.message}-${i}`; }

// ── Styled ─────────────────────────────────
const Page = styled.div`min-height: 100vh; background: #faf9f7;`;
const HeaderBar = styled.div`position: sticky; top: 0; z-index: 30; border-bottom: 1px solid ${(p) => p.theme.colors.border}; background: ${(p) => p.theme.colors.neutralSurface}; padding: 16px;`;
const HeaderInner = styled.div`max-width: 72rem; margin: 0 auto; display: flex; align-items: center; justify-content: space-between;`;
const Title = styled.h1`font-size: ${(p) => p.theme.fontSizes.sm}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary};`;
const Subtitle = styled.p`font-size: 11px; color: #a8a29e; margin: 2px 0 0;`;
const Main = styled.main`max-width: 72rem; margin: 0 auto; padding: 24px 16px; display: flex; flex-direction: column; gap: 16px;`;

const Section = styled.div`background: ${(p) => p.theme.colors.neutralSurface}; border: 1px solid ${(p) => p.theme.colors.border}; border-radius: ${(p) => p.theme.radii.lg}; overflow: hidden;`;
const SectionH2 = styled.h2`font-size: ${(p) => p.theme.fontSizes.xs}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary}; text-transform: uppercase; letter-spacing: 0.1em;`;
const SecHeader = styled.div`padding: 12px 16px; border-bottom: 1px solid ${(p) => p.theme.colors.border}; display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px;`;
const SecBody = styled.div`padding: 12px 16px;`;

const Table = styled.table`width: 100%; font-size: ${(p) => p.theme.fontSizes.xs};`;
const Th = styled.th`padding: 8px 12px; text-align: left; font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em; background: ${(p) => p.theme.colors.neutralSurface};`;
const Tr = styled.tr`border-bottom: 1px solid #f7f5f0; &:hover { background: #faf9f7; }`;
const ExpandedTr = styled.tr`background: #faf9f7; border-bottom: 1px solid ${(p) => p.theme.colors.border};`;
const Td = styled.td`padding: 8px 12px;`;
const Mono = styled.span`font-family: ${(p) => p.theme.fonts.mono};`;

const ErrorBar = styled.div`padding: 8px 16px; font-size: 11px; color: #dc2626; border-top: 1px solid #fecaca; background: #fef2f2;`;

const FormRow = styled.form`padding: 12px 16px; border-bottom: 1px solid ${(p) => p.theme.colors.borderLight}; display: grid; grid-template-columns: 1fr; gap: 12px; align-items: end; @media (min-width: 1024px) { grid-template-columns: 0.9fr 0.45fr 1.1fr auto; }`;

const Label = styled.label`display: block;`;
const LabelText = styled.span`font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em;`;
const Field = styled.input`margin-top: 4px; width: 100%; padding: 6px 8px; font-size: ${(p) => p.theme.fontSizes.xs}; border: 1px solid ${(p) => p.theme.colors.border}; border-radius: ${(p) => p.theme.radii.sm}; background: ${(p) => p.theme.colors.neutralSurface}; color: ${(p) => p.theme.colors.textPrimary}; &:focus { outline: none; border-color: #14b8a6; }`;



const Badge = styled.span<{ $bg: string; $color: string }>`display: inline-block; padding: 2px 6px; border-radius: ${(p) => p.theme.radii.sm}; font-size: 10px; font-weight: 600; background: ${(p) => p.$bg}; color: ${(p) => p.$color};`;

const CountryCell = styled.div`font-size: 11px;`;
const CountryName = styled.div`font-size: 10px; color: #a8a29e; margin-top: 2px; max-width: 160px; overflow: hidden; text-overflow: ellipsis;`;
const ExpGrid = styled.div`display: grid; grid-template-columns: 1fr 1fr; gap: 16px; font-size: 11px; @media (min-width: 640px) { grid-template-columns: repeat(4, 1fr); }`;
const ExpItem = styled.div``;
const ExpLabel = styled.div`font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase;`;
const ExpValue = styled.div`color: #57534e;`;
const Truncate = styled.div`max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;`;

const LogScroll = styled.div`max-height: 28rem; overflow: auto;`;
const LogMessage = styled.div`max-width: 56rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;`;
const StickyThead = styled.thead`position: sticky; top: 0; z-index: 10; background: ${(p) => p.theme.colors.neutralSurface};`;

const StatGrid = styled.div`display: grid; grid-template-columns: 1fr 1fr; gap: 12px; @media (min-width: 640px) { grid-template-columns: repeat(4, 1fr); } @media (min-width: 1024px) { grid-template-columns: repeat(8, 1fr); }`;
const StatItem = styled.div``;
const StatLabel = styled.div`font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em;`;
const StatValue = styled.div`margin-top: 2px; font-size: ${(p) => p.theme.fontSizes.sm}; font-family: ${(p) => p.theme.fonts.mono}; color: #292524;`;

const StepGrid = styled.div`display: grid; grid-template-columns: 1fr 1fr; gap: 12px; font-size: 11px; @media (min-width: 640px) { grid-template-columns: repeat(5, 1fr); }`;

const VersionRow = styled.div`display: flex; flex-wrap: wrap; align-items: center; gap: 16px;`;
const VerBlock = styled.div``;
const VerLabel = styled.div`font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em;`;
const VerValue = styled.div`margin-top: 2px; font-size: ${(p) => p.theme.fontSizes.sm}; font-family: ${(p) => p.theme.fonts.mono}; color: #292524;`;

const InstallRow = styled.form`display: flex; align-items: end; gap: 12px;`;
const ErrorText = styled.div`margin-top: 8px; font-size: 11px; color: #dc2626;`;

// ── Helpers ────────────────────────────────
function levelColor(lvl?: string) { return lvl?.toLowerCase() === 'error' || lvl?.toLowerCase() === 'fatal' ? '#dc2626' : lvl?.toLowerCase() === 'warning' || lvl?.toLowerCase() === 'warn' ? '#d97706' : lvl?.toLowerCase() === 'info' ? '#0f766e' : '#78716c'; }
function originBadge(origin: string) { const o = origin.toLowerCase(); return o === 'cscli' ? { text: 'Manual', bg: '#ede9fe', c: '#6d28d9' } : o === 'crowdsec' ? { text: 'Engine', bg: '#ccfbf1', c: '#0f766e' } : o === 'capi' ? { text: 'Community', bg: '#dbeafe', c: '#1d4ed8' } : { text: origin, bg: '#f5f5f4', c: '#57534e' }; }

export function CrowdSecPage() {
  const { data: status } = useCrowdSecStatus();
  const { data: metrics } = useCrowdSecMetrics();
  const { data: decisions, isLoading: dl, error: de } = useCrowdSecDecisions();
  const { data: logs, isLoading: ll, error: le } = useCrowdSecLogs();
  const addDecision = useAddCrowdSecDecision();
  const deleteDecision = useDeleteCrowdSecDecision();
  const { data: hubData, isLoading: hl } = useCrowdSecHub();
  const { data: version } = useCrowdSecVersion();
  const updateCs = useUpdateCrowdSec();
  const installHub = useInstallCrowdSecHubItem();
  const removeHub = useRemoveCrowdSecHubItem();
  const [ip, setIP] = useState('');
  const [duration, setDuration] = useState('4h');
  const [reason, setReason] = useState('manual ban from homectl');
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [hubType, setHubType] = useState('collection');
  const [hubInstallName, setHubInstallName] = useState('');
  const [hubError, setHubError] = useState<string | null>(null);

  const hubItems = useMemo(() => hubData?.items ?? [], [hubData]);
  const installedItems = useMemo(() => hubItems.filter((h) => h.installed), [hubItems]);
  const installedByType = useMemo(() => { const g: Record<string, CrowdSecHubItem[]> = {}; for (const i of installedItems) { if (!g[i.type]) g[i.type] = []; g[i.type].push(i); } return g; }, [installedItems]);
  const activeBans = (decisions ?? []).filter((d) => d.type === 'ban').length;

  useEffect(() => {
    const err = de || le || addDecision.error || deleteDecision.error;
    if (!err) return;
    setError(err instanceof Error ? err.message.trim() : 'CrowdSec request failed');
  }, [addDecision.error, de, deleteDecision.error, le]);

  function submitHubInstall(e: FormEvent) { e.preventDefault(); if (!hubInstallName.trim()) return; setHubError(null); installHub.mutateAsync({ type: hubType, name: hubInstallName.trim() }).then(() => setHubInstallName('')).catch((err) => setHubError(err instanceof Error ? err.message.trim() : 'Install failed')); }
  function submitHubRemove(item: CrowdSecHubItem) { if (!window.confirm(`Remove ${item.name}?`)) return; removeHub.mutate({ type: item.type, name: item.name }); }
  function submitBan(e: FormEvent) { e.preventDefault(); setError(null); addDecision.mutateAsync({ ip: ip.trim(), duration: duration.trim(), reason: reason.trim() }).then(() => setIP('')).catch((err) => setError(err instanceof Error ? err.message.trim() : 'CrowdSec request failed')); }
  function removeBan(d: CrowdSecDecision) { if (!d.id || !window.confirm(`Delete CrowdSec ban for ${d.value}?`)) return; deleteDecision.mutate(d.id); }
  function toggleExpand(d: CrowdSecDecision) { setExpanded((p) => { const n = new Set(p); n.has(decisionKey(d)) ? n.delete(decisionKey(d)) : n.add(decisionKey(d)); return n; }); }

  return (
    <Page>
      <HeaderBar><HeaderInner><div><Title>CrowdSec</Title><Subtitle>{dl ? 'Loading...' : `${activeBans} active ban${activeBans === 1 ? '' : 's'}`}</Subtitle></div></HeaderInner></HeaderBar>
      <Main>
        {/* Status */}
        <Section>
          <SecHeader>
            <SectionH2>Configuration</SectionH2>
            <span style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.1em', color: status?.state === 'error' ? '#dc2626' : status?.state === 'ready' ? '#047857' : '#a8a29e' }}>{status?.state ?? 'idle'}</span>
          </SecHeader>
          <SecBody>
            <StepGrid>
              {[
                { k: 'config', l: 'Config', r: !!status?.config_ready },
                { k: 'hub', l: 'Collections', r: !!status?.hub_ready },
                { k: 'engine', l: 'Engine', r: !!status?.engine_running },
                { k: 'bouncer', l: 'Bouncer', r: !!status?.bouncer_ready },
                { k: 'caddy', l: 'Caddy', r: !!status?.caddy_integrated },
              ].map((s) => {
                const active = status?.step === s.k;
                const err = status?.state === 'error' && active;
                const color = err ? '#dc2626' : s.r ? '#10b981' : active ? '#fbbf24' : '#d6d3d1';
                return (
                  <div key={s.k} style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                    <span style={{ display: 'inline-block', width: 8, height: 8, borderRadius: '50%', background: color, flexShrink: 0 }} />
                    <span style={{ color: s.r ? '#57534e' : active ? '#b45309' : err ? '#dc2626' : '#a8a29e' }}>{s.l}</span>
                  </div>
                );
              })}
            </StepGrid>
            <div style={{ fontSize: 11, marginTop: 12, color: status?.state === 'error' ? '#dc2626' : '#78716c' }}>{status?.error || status?.message || 'Waiting for CrowdSec bootstrap status.'}</div>
          </SecBody>
        </Section>

        {/* Version */}
        {version?.current && (
          <Section>
            <SecHeader>
              <SectionH2>Version</SectionH2>
              <Badge $bg={version.update_available ? '#fef3c7' : '#ecfdf5'} $color={version.update_available ? '#b45309' : '#059669'}>{version.update_available ? 'Update available' : 'Up to date'}</Badge>
            </SecHeader>
            <SecBody>
              <VersionRow>
                <VerBlock><VerLabel>Installed</VerLabel><VerValue>v{version.current}</VerValue></VerBlock>
                {version.latest && <VerBlock><VerLabel>Latest</VerLabel><VerValue style={{ color: '#78716c' }}>v{version.latest}</VerValue></VerBlock>}
                {version.update_available && <Button variant="primary" size="sm" onClick={() => { if (!window.confirm(`Update CrowdSec from v${version.current} to v${version.latest}?`)) return; updateCs.mutateAsync().catch((err) => setHubError(err instanceof Error ? err.message.trim() : 'Update failed')); }} disabled={updateCs.isPending} style={{ marginLeft: 'auto' }}>{updateCs.isPending ? 'Updating...' : 'Update CrowdSec'}</Button>}
              </VersionRow>
              {hubError && <ErrorText>{hubError}</ErrorText>}
            </SecBody>
          </Section>
        )}

        {/* Manual ban */}
        <Section>
          <SecHeader><SectionH2>Manual ban</SectionH2></SecHeader>
          <FormRow onSubmit={submitBan}>
            <Label><LabelText>IP address</LabelText><Field value={ip} onChange={(e) => setIP(e.target.value)} placeholder="203.0.113.10" /></Label>
            <Label><LabelText>Duration</LabelText><Field value={duration} onChange={(e) => setDuration(e.target.value)} placeholder="4h" /></Label>
            <Label><LabelText>Reason</LabelText><Field value={reason} onChange={(e) => setReason(e.target.value)} /></Label>
            <Button variant="primary" size="sm" type="submit" disabled={addDecision.isPending || !ip.trim()}>{addDecision.isPending ? 'Adding...' : 'Add ban'}</Button>
          </FormRow>
          {error && <ErrorBar>{error}</ErrorBar>}
        </Section>

        {/* Decisions */}
        <Section>
          <SecHeader><SectionH2>Current bans</SectionH2></SecHeader>
          <div style={{ overflowX: 'auto' }}>
            <Table>
              <StickyThead><tr><Th style={{ width: 32 }}></Th><Th>IP</Th><Th>Country</Th><Th>Reason</Th><Th>Origin</Th><Th>Events</Th><Th>Until</Th><Th></Th></tr></StickyThead>
              <tbody>
                {(decisions ?? []).map((d) => {
                  const isExp = expanded.has(decisionKey(d));
                  const bag = originBadge(d.origin || '');
                  return (
                    <>
                      <Tr key={decisionKey(d)}>
                        <Td><button onClick={() => toggleExpand(d)} title={isExp ? 'Collapse' : 'Expand'} style={{ fontSize: 11, border: 'none', background: 'none', cursor: 'pointer', padding: 0, transform: isExp ? 'rotate(90deg)' : 'none', transition: 'transform 0.15s' }}>&#9654;</button></Td>
                        <Td><Mono style={{ fontSize: 11, color: '#57534e' }}>{d.value}</Mono></Td>
                        <Td>
                          <CountryCell>{countryFlag(d.country)} <span style={{ color: '#57534e' }}>{d.country}</span>{d.as_name && <CountryName>{d.as_name}</CountryName>}</CountryCell>
                        </Td>
                        <Td><Truncate title={d.reason || d.scenario || ''}>{d.reason || d.scenario || '--'}</Truncate></Td>
                        <Td><Badge $bg={bag.bg} $color={bag.c}>{bag.text}</Badge></Td>
                        <Td style={{ color: '#78716c' }}>{d.event_count > 0 ? d.event_count : '--'}</Td>
                        <Td style={{ color: '#78716c' }}>{formatDuration(d.duration)}{d.until && <div style={{ fontSize: 10, color: '#a8a29e' }}>{formatDateTime(d.until)}</div>}</Td>
                        <Td style={{ textAlign: 'right' }}><Button variant="destructive" size="sm" onClick={() => removeBan(d)} disabled={deleteDecision.isPending || d.scope.toLowerCase() !== 'ip'}>Delete</Button></Td>
                      </Tr>
                      {isExp && (
                        <ExpandedTr key={`${decisionKey(d)}-x`}>
                          <td colSpan={8} style={{ padding: '12px 24px' }}>
                            <ExpGrid>
                              {[
                                ['Scope', d.scope || '--'], ['Type', d.type || '--'], ['Source Range', d.source_range || '--'],
                                ['ASN', d.as_number ? `AS${d.as_number}` : '--'], ['Start', formatDateTime(d.start_at)], ['Stop', formatDateTime(d.stop_at)],
                                ['Scenario', d.scenario || '--'], ['Duration', d.duration || '--'],
                              ].map(([k, v]) => (
                                <ExpItem key={k}><ExpLabel>{k}</ExpLabel><ExpValue>{v}{d.as_name && k === 'ASN' && <span style={{ color: '#a8a29e' }}> {d.as_name}</span>}</ExpValue></ExpItem>
                              ))}
                            </ExpGrid>
                          </td>
                        </ExpandedTr>
                      )}
                    </>
                  );
                })}
                {!dl && (decisions ?? []).length === 0 && <tr><td colSpan={8} style={{ padding: '32px 16px', textAlign: 'center', fontSize: 11, color: '#a8a29e' }}>No active CrowdSec bans.</td></tr>}
                {dl && <tr><td colSpan={8} style={{ padding: '32px 16px', textAlign: 'center', fontSize: 11, color: '#a8a29e' }}>Loading bans...</td></tr>}
              </tbody>
            </Table>
          </div>
        </Section>

        {/* Metrics */}
        {metrics && (
          <Section>
            <SecHeader><SectionH2>Metrics</SectionH2></SecHeader>
            <SecBody>
              <StatGrid>
                {[
                  ['Lines Read', metrics.acquisition_lines_read], ['Lines Parsed', metrics.acquisition_lines_parsed], ['Poured', metrics.acquisition_lines_pour],
                  ['Alerts', metrics.alerts_count], ['Active Bans', metrics.active_bans], ['Scenarios', metrics.active_scenarios],
                  ['Parser Hits', metrics.parser_hits_total], ['Parser Parsed', metrics.parser_parsed_total],
                ].map(([l, v]) => <StatItem key={l}><StatLabel>{l}</StatLabel><StatValue>{v.toLocaleString()}</StatValue></StatItem>)}
              </StatGrid>
            </SecBody>
          </Section>
        )}

        {/* Hub */}
        <Section>
          <SecHeader>
            <SectionH2>Hub</SectionH2>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              {(['collection', 'scenario', 'parser'] as const).map((t) => <Button key={t} variant={hubType === t ? 'primary' : 'ghost'} size="sm" onClick={() => setHubType(t)}>{t === 'collection' ? 'Collections' : t === 'scenario' ? 'Scenarios' : 'Parsers'}</Button>)}
            </div>
          </SecHeader>
          <SecBody>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 12, alignItems: 'end', fontSize: 12, color: '#78716c', marginBottom: 12 }}>
              <span>{(installedByType[hubType] ?? []).length} installed{hubType === 'collection' ? ' / 6 installed' : ''}</span><span /><span style={{ textAlign: 'right' }}>{hl && 'Loading...'}</span>
            </div>
          </SecBody>
          <div style={{ overflowX: 'auto' }}>
            <Table>
              <thead><tr style={{ borderTop: '1px solid #e2ddd7' }}><Th>Name</Th><Th>Version</Th><Th>Status</Th><Th>Description</Th><Th></Th></tr></thead>
              <tbody>
                {(installedByType[hubType] ?? []).map((item) => (
                  <Tr key={`${item.type}-${item.name}`}>
                    <Td><Mono style={{ fontSize: 11, color: '#57534e' }}>{item.name}</Mono></Td>
                    <Td style={{ color: '#78716c' }}>{item.version || '--'}</Td>
                    <Td><Badge $bg={item.update_available ? '#fef3c7' : '#ecfdf5'} $color={item.update_available ? '#b45309' : '#059669'}>{item.update_available ? 'Update available' : 'Installed'}</Badge></Td>
                    <Td><Truncate title={item.description} style={{ maxWidth: 300 }}>{item.description || '--'}</Truncate></Td>
                    <Td style={{ textAlign: 'right' }}><Button variant="destructive" size="sm" onClick={() => submitHubRemove(item)} disabled={removeHub.isPending}>Remove</Button></Td>
                  </Tr>
                ))}
                {(installedByType[hubType] ?? []).length === 0 && !hl && <tr><td colSpan={5} style={{ padding: '16px', textAlign: 'center', fontSize: 11, color: '#a8a29e' }}>No installed {hubType}s.</td></tr>}
              </tbody>
            </Table>
          </div>
          <div style={{ padding: '12px 16px', borderTop: '1px solid #e2ddd7' }}>
            <InstallRow onSubmit={submitHubInstall}>
              <Label style={{ flex: 1 }}>
                <LabelText>Install {hubType === 'collection' ? 'collection' : hubType === 'scenario' ? 'scenario' : 'parser'}</LabelText>
                <Field value={hubInstallName} onChange={(e) => setHubInstallName(e.target.value)} placeholder={hubType === 'collection' ? 'crowdsecurity/nginx' : hubType === 'scenario' ? 'crowdsecurity/ssh-bf' : 'crowdsecurity/nginx-logs'} />
              </Label>
              <Button variant="primary" size="sm" type="submit" disabled={installHub.isPending || !hubInstallName.trim()} style={{ whiteSpace: 'nowrap' }}>{installHub.isPending ? 'Installing...' : 'Install'}</Button>
            </InstallRow>
            {hubError && <ErrorText>{hubError}</ErrorText>}
          </div>
        </Section>

        {/* Logs */}
        <Section>
          <SecHeader><SectionH2>CrowdSec logs</SectionH2></SecHeader>
          <LogScroll>
            <Table>
              <StickyThead><tr><Th>Time</Th><Th>Source</Th><Th>Level</Th><Th>Message</Th></tr></StickyThead>
              <tbody>
                {(logs ?? []).map((e, i) => (
                  <Tr key={logKey(e, i)}>
                    <Td style={{ color: '#78716c', whiteSpace: 'nowrap' }}>{formatDateTime(e.timestamp)}</Td>
                    <Td><Mono style={{ fontSize: 11, color: '#57534e' }}>{e.source}</Mono></Td>
                    <Td style={{ fontWeight: 600, color: levelColor(e.level) }}>{e.level || '--'}</Td>
                    <Td><LogMessage title={e.raw}>{e.message || e.raw}</LogMessage></Td>
                  </Tr>
                ))}
                {!ll && (logs ?? []).length === 0 && <tr><td colSpan={4} style={{ padding: '32px 16px', textAlign: 'center', fontSize: 11, color: '#a8a29e' }}>No CrowdSec logs found.</td></tr>}
                {ll && <tr><td colSpan={4} style={{ padding: '32px 16px', textAlign: 'center', fontSize: 11, color: '#a8a29e' }}>Loading CrowdSec logs...</td></tr>}
              </tbody>
            </Table>
          </LogScroll>
        </Section>
      </Main>
    </Page>
  );
}
