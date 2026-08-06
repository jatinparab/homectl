import { useEffect, useMemo, useState } from 'react';
import type { FormEvent } from 'react';
import styled from '@emotion/styled';
import { useCreatePublicRoute, useDeletePublicRoute, usePublicRoutes, useUpdatePublicRoute } from '../../api/hooks';
import type { PublicRoute, PublicRouteAuthMode, PublicRouteMode } from '../../api/types';
import { DesiredToggle } from '../../features/shared/components/StatusBadge';
import { Button } from '../../components/ui';

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}
function formatTime(value?: string): string {
  if (!value) return '--';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? '--' : d.toLocaleTimeString();
}
function authLabel(mode?: PublicRouteAuthMode): string {
  if (mode === 'authentik') return 'Authentik';
  if (mode === 'public') return 'Public';
  return 'App login';
}

const emptyForm = { id: '', host: '', mode: 'homelab' as PublicRouteMode, auth_mode: 'upstream' as PublicRouteAuthMode, custom_target: '', enabled: true };

// ── Styled ─────────────────────────────────
const Page = styled.div`min-height: 100vh; background: #faf9f7;`;
const HeaderBar = styled.div`position: sticky; top: 0; z-index: 30; border-bottom: 1px solid ${(p) => p.theme.colors.border}; background: ${(p) => p.theme.colors.neutralSurface}; padding: 16px;`;
const HeaderInner = styled.div`max-width: 72rem; margin: 0 auto; display: flex; align-items: center; justify-content: space-between;`;
const Title = styled.h1`font-size: ${(p) => p.theme.fontSizes.sm}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary};`;
const Subtitle = styled.p`font-size: 11px; color: #a8a29e; margin: 2px 0 0;`;
const Main = styled.main`max-width: 72rem; margin: 0 auto; padding: 24px 16px; display: flex; flex-direction: column; gap: 16px;`;

const Section = styled.div`background: ${(p) => p.theme.colors.neutralSurface}; border: 1px solid ${(p) => p.theme.colors.border}; border-radius: ${(p) => p.theme.radii.lg}; overflow: hidden;`;
const SectionHeader = styled.div`padding: 12px 16px; border-bottom: 1px solid ${(p) => p.theme.colors.border}; display: flex; align-items: center; justify-content: space-between;`;
const SectionTitle = styled.h2`font-size: ${(p) => p.theme.fontSizes.xs}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary}; text-transform: uppercase; letter-spacing: 0.1em;`;
const SectionSub = styled.p`font-size: 11px; color: #a8a29e; margin-top: 4px;`;

const FormRow = styled.form`
  padding: 12px 16px;
  border-bottom: 1px solid ${(p) => p.theme.colors.borderLight};
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
  align-items: end;
  @media (min-width: 1024px) { grid-template-columns: 1.1fr 0.8fr 0.8fr 1.1fr auto; }
`;

const Label = styled.label`display: block;`;
const LabelText = styled.span`font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em;`;
const LabelHelp = styled.p`font-size: 10px; color: #a8a29e; margin-top: 4px;`;
const Field = styled.input`
  margin-top: 4px; width: 100%; padding: 6px 8px; font-size: ${(p) => p.theme.fontSizes.xs};
  border: 1px solid ${(p) => p.theme.colors.border}; border-radius: ${(p) => p.theme.radii.sm};
  background: ${(p) => p.theme.colors.neutralSurface}; color: ${(p) => p.theme.colors.textPrimary};
  &:focus { outline: none; border-color: #14b8a6; }
  &:disabled { background: #fafaf9; color: #d6d3d1; }
`;
const SelectField = styled.select`
  margin-top: 4px; width: 100%; padding: 6px 8px; font-size: ${(p) => p.theme.fontSizes.xs};
  border: 1px solid ${(p) => p.theme.colors.border}; border-radius: ${(p) => p.theme.radii.sm};
  background: ${(p) => p.theme.colors.neutralSurface}; color: ${(p) => p.theme.colors.textPrimary};
  &:focus { outline: none; border-color: #14b8a6; }
`;

const FormActions = styled.div`display: flex; align-items: center; gap: 8px;`;


const ErrorBar = styled.div`padding: 8px 16px; font-size: 11px; color: #dc2626; border-bottom: 1px solid #fecaca; background: #fef2f2;`;

const Table = styled.table`width: 100%; font-size: ${(p) => p.theme.fontSizes.xs};`;
const Th = styled.th`padding: 10px 12px; text-align: left; font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em;`;
const Tr = styled.tr`border-bottom: 1px solid #f7f5f0; &:hover { background: #faf9f7; }`;
const Td = styled.td`padding: 10px 12px;`;
const HostName = styled.div`font-weight: 600; color: ${(p) => p.theme.colors.textPrimary};`;
const HostMode = styled.div`font-size: 11px; color: #a8a29e;`;
const AuthBadge = styled.span<{ $mode?: PublicRouteAuthMode }>`
  display: inline-flex;
  align-items: center;
  min-width: 5.75rem;
  justify-content: center;
  border-radius: 3px;
  padding: 3px 7px;
  font-size: 10px;
  font-weight: 700;
  line-height: 1.2;
  color: ${(p) => (p.$mode === 'authentik' ? '#0f766e' : p.$mode === 'public' ? '#b45309' : '#57534e')};
  background: ${(p) => (p.$mode === 'authentik' ? '#ccfbf1' : p.$mode === 'public' ? '#fef3c7' : '#f5f5f4')};
  border: 1px solid ${(p) => (p.$mode === 'authentik' ? '#99f6e4' : p.$mode === 'public' ? '#fde68a' : '#e7e5e4')};
`;
const StatusDot = styled.span<{ $working: boolean; $enabled: boolean }>`
  display: inline-block; width: 8px; height: 8px; border-radius: 50%;
  background: ${(p) => (p.$working ? '#10b981' : p.$enabled ? '#ef4444' : '#d6d3d1')};
  ${(p) => p.$working && 'box-shadow: 0 0 6px rgba(16,185,129,0.5);'};
`;
const StatusText = styled.span<{ $working: boolean; $enabled: boolean }>`
  color: ${(p) => (p.$working ? '#047857' : p.$enabled ? '#dc2626' : '#a8a29e')};
`;
const CheckInfo = styled.div`font-size: 10px; color: #a8a29e; margin-top: 2px;`;
const Target = styled.div`font-family: ${(p) => p.theme.fonts.mono}; font-size: 11px; color: #57534e; max-width: 20rem; overflow: hidden; text-overflow: ellipsis;`;
const Traffic = styled.div`color: #57534e;`;
const TrafficSub = styled.div`font-size: 11px; color: #a8a29e;`;
const RowActions = styled.div`display: flex; align-items: center; justify-content: flex-end; gap: 8px;`;

const EmptyRow = styled.div`padding: 32px 16px; text-align: center; font-size: 11px; color: #a8a29e;`;

export function RoutingPage() {
  const { data: routes, isLoading } = usePublicRoutes();
  const createRoute = useCreatePublicRoute();
  const updateRoute = useUpdatePublicRoute();
  const deleteRoute = useDeletePublicRoute();
  const [form, setForm] = useState(emptyForm);
  const [error, setError] = useState<string | null>(null);

  const sortedRoutes = useMemo(() => [...(routes ?? [])].sort((a, b) => a.host.localeCompare(b.host)), [routes]);

  useEffect(() => {
    const err = createRoute.error || updateRoute.error || deleteRoute.error;
    if (!err) return;
    setError(err instanceof Error ? err.message.trim() : 'Route update failed');
  }, [createRoute.error, updateRoute.error, deleteRoute.error]);

  const saving = createRoute.isPending || updateRoute.isPending;
  function resetForm() { setForm(emptyForm); setError(null); }
  function editRoute(route: PublicRoute) { setForm({ id: route.id, host: route.host, mode: route.mode, auth_mode: route.auth_mode ?? 'upstream', custom_target: route.custom_target ?? '', enabled: route.enabled }); setError(null); }
  function submitRoute(event: FormEvent) {
    event.preventDefault(); setError(null);
    const payload = { host: form.host.trim(), mode: form.mode, auth_mode: form.auth_mode, custom_target: form.mode === 'custom' ? form.custom_target.trim() : '', enabled: form.enabled };
    (form.id ? updateRoute.mutateAsync({ id: form.id, route: payload }) : createRoute.mutateAsync(payload))
      .then(resetForm).catch((err) => setError(err instanceof Error ? err.message.trim() : 'Route update failed'));
  }
  function toggleRoute(route: PublicRoute) { updateRoute.mutate({ id: route.id, route: { host: route.host, mode: route.mode, auth_mode: route.auth_mode ?? 'upstream', custom_target: route.custom_target, enabled: !route.enabled } }); }
  function removeRoute(route: PublicRoute) { if (!window.confirm(`Delete public route ${route.host}?`)) return; deleteRoute.mutate(route.id); }

  return (
    <Page>
      <HeaderBar><HeaderInner><div><Title>Routing</Title><Subtitle>{isLoading ? 'Loading...' : `${sortedRoutes.length} public route${sortedRoutes.length !== 1 ? 's' : ''}`}</Subtitle></div></HeaderInner></HeaderBar>
      <Main>
        <Section>
          <SectionHeader><div><SectionTitle>Public routes</SectionTitle><SectionSub>Manager Caddy forwards homelab routes to the elected Caddy gateway.</SectionSub></div></SectionHeader>

          <FormRow onSubmit={submitRoute}>
            <Label><LabelText>Host</LabelText><Field value={form.host} onChange={(e) => setForm((v) => ({ ...v, host: e.target.value }))} placeholder="photos.your-domain.example" /></Label>
            <Label>
              <LabelText>Destination</LabelText>
              <SelectField value={form.mode} onChange={(e) => setForm((v) => ({ ...v, mode: e.target.value as PublicRouteMode }))}><option value="homelab">Homelab gateway</option><option value="custom">Custom URL</option></SelectField>
            </Label>
            <Label>
              <LabelText>Auth</LabelText>
              <SelectField value={form.auth_mode} onChange={(e) => setForm((v) => ({ ...v, auth_mode: e.target.value as PublicRouteAuthMode }))}><option value="authentik">Authentik</option><option value="upstream">App login</option><option value="public">Public</option></SelectField>
            </Label>
            <Label>
              <LabelText>Custom URL</LabelText>
              <Field value={form.custom_target} onChange={(e) => setForm((v) => ({ ...v, custom_target: e.target.value }))} placeholder="http://homectl-localhost:3001" disabled={form.mode !== 'custom'} />
              {form.mode === 'custom' && <LabelHelp>For VPS-local services, use <span style={{ fontFamily: 'monospace' }}>homectl-localhost</span> instead of <span style={{ fontFamily: 'monospace' }}>localhost</span>.</LabelHelp>}
            </Label>
            <FormActions>
              <DesiredToggle state={form.enabled ? 'running' : 'stopped'} onToggle={() => setForm((v) => ({ ...v, enabled: !v.enabled }))} disabled={saving} />
              <Button type="submit" variant="primary" size="sm" disabled={saving || !form.host.trim()}>{saving ? 'Saving...' : form.id ? 'Update' : 'Add'}</Button>
              {form.id && <Button type="button" variant="secondary" size="sm" onClick={resetForm}>Cancel</Button>}
            </FormActions>
          </FormRow>

          {error && <ErrorBar>{error}</ErrorBar>}

          <div style={{ overflowX: 'auto' }}>
            <Table>
              <thead><tr><Th>Route</Th><Th>Auth</Th><Th>Status</Th><Th>Target</Th><Th>Traffic</Th><Th></Th></tr></thead>
              <tbody>
                {sortedRoutes.map((route) => (
                  <Tr key={route.id}>
                    <Td><HostName>{route.host}</HostName><HostMode>{route.mode === 'homelab' ? 'homelab gateway' : 'custom route'}</HostMode></Td>
                    <Td><AuthBadge $mode={route.auth_mode ?? 'upstream'}>{authLabel(route.auth_mode)}</AuthBadge></Td>
                    <Td>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <StatusDot $working={route.health.working} $enabled={route.enabled} />
                        <StatusText $working={route.health.working} $enabled={route.enabled}>{route.enabled ? route.health.status || 'unknown' : 'disabled'}</StatusText>
                      </div>
                      <CheckInfo>{route.health.http_status ? `HTTP ${route.health.http_status}` : route.health.last_error || 'Last check'} · {formatTime(route.health.checked_at)}</CheckInfo>
                    </Td>
                    <Td><Target>{route.health.resolved_to || route.custom_target || 'gateway pending'}</Target></Td>
                    <Td><Traffic>{route.health.request_count} req</Traffic><TrafficSub>{formatBytes(route.health.response_bytes)} out</TrafficSub></Td>
                    <Td>
                      <RowActions>
                        <DesiredToggle state={route.enabled ? 'running' : 'stopped'} onToggle={() => toggleRoute(route)} disabled={updateRoute.isPending} />
                        <Button variant="secondary" size="sm" onClick={() => editRoute(route)}>Edit</Button>
                        <Button variant="destructive" size="sm" onClick={() => removeRoute(route)} disabled={deleteRoute.isPending}>Delete</Button>
                      </RowActions>
                    </Td>
                  </Tr>
                ))}
                {!isLoading && sortedRoutes.length === 0 && <tr><td colSpan={6}><EmptyRow>No public routes configured.</EmptyRow></td></tr>}
              </tbody>
            </Table>
          </div>
        </Section>
      </Main>
    </Page>
  );
}
