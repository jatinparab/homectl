import { useCallback, useEffect, useState } from 'react';
import styled from '@emotion/styled';
import { Button, Input } from '../../components/ui';
import { fetchAPIKeys, createAPIKey, deleteAPIKey } from '../../api/client';
import type { APIKey, APIKeyCreated } from '../../api/client';
import { Copy, Trash2, Key } from 'lucide-react';

function formatDate(v?: string) {
  if (!v) return '--';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString();
}

const Page = styled.div`min-height: 100vh; background: ${(p) => p.theme.colors.neutralBg};`;
const HeaderBar = styled.div`position: sticky; top: 0; z-index: 30; border-bottom: 1px solid ${(p) => p.theme.colors.border}; background: ${(p) => p.theme.colors.neutralSurface}; padding: 16px;`;
const HeaderInner = styled.div`max-width: 72rem; margin: 0 auto; display: flex; align-items: center; justify-content: space-between;`;
const Title = styled.h1`font-size: ${(p) => p.theme.fontSizes.sm}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary};`;
const Main = styled.main`max-width: 72rem; margin: 0 auto; padding: 24px 16px; display: flex; flex-direction: column; gap: 16px;`;
const Section = styled.div`background: ${(p) => p.theme.colors.neutralSurface}; border: 1px solid ${(p) => p.theme.colors.border}; border-radius: ${(p) => p.theme.radii.lg}; overflow: hidden;`;
const SectionH2 = styled.h2`font-size: ${(p) => p.theme.fontSizes.xs}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary}; text-transform: uppercase; letter-spacing: 0.1em;`;
const SecHeader = styled.div`padding: 12px 16px; border-bottom: 1px solid ${(p) => p.theme.colors.border}; display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px;`;
const SecBody = styled.div`padding: 12px 16px;`;

const Table = styled.table`width: 100%; font-size: ${(p) => p.theme.fontSizes.xs};`;
const Th = styled.th`padding: 8px 12px; text-align: left; font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em; background: ${(p) => p.theme.colors.neutralSurface};`;
const Tr = styled.tr`border-bottom: 1px solid #f7f5f0; &:hover { background: #faf9f7; }`;
const Td = styled.td`padding: 8px 12px;`;
const Mono = styled.span`font-family: ${(p) => p.theme.fonts.mono}; font-size: 11px;`;

const CreateRow = styled.div`display: flex; gap: 8px; align-items: flex-end; max-width: 400px;`;
const KeyReveal = styled.div`margin-top: 12px; padding: 12px; background: #fef9c3; border: 1px solid #facc15; border-radius: ${(p) => p.theme.radii.md};`;
const KeyRevealLabel = styled.div`font-size: 10px; font-weight: 600; color: #a16207; text-transform: uppercase; letter-spacing: 0.1em; margin-bottom: 6px;`;
const KeyRevealValue = styled.div`display: flex; align-items: center; gap: 8px;`;
const KeyCode = styled.code`font-family: ${(p) => p.theme.fonts.mono}; font-size: 11px; word-break: break-all; flex: 1;`;
const CopyBtn = styled.button`display: flex; align-items: center; gap: 4px; padding: 4px 8px; font-size: 10px; border: 1px solid ${(p) => p.theme.colors.border}; border-radius: ${(p) => p.theme.radii.sm}; background: ${(p) => p.theme.colors.neutralSurface}; color: ${(p) => p.theme.colors.textPrimary}; cursor: pointer; &:hover { background: ${(p) => p.theme.colors.neutralSurfaceAlt}; }`;
const DeleteBtn = styled.button`display: flex; align-items: center; gap: 4px; padding: 4px 8px; font-size: 10px; border: 1px solid #fecaca; border-radius: ${(p) => p.theme.radii.sm}; background: transparent; color: #dc2626; cursor: pointer; &:hover { background: #fef2f2; }`;

const EmptyText = styled.p`font-size: ${(p) => p.theme.fontSizes.xs}; color: ${(p) => p.theme.colors.textMuted}; padding: 8px 0;`;

export function SettingsPage() {
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<APIKeyCreated | null>(null);
  const [copied, setCopied] = useState(false);

  const loadKeys = useCallback(async () => {
    try {
      setKeys(await fetchAPIKeys());
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void loadKeys(); }, [loadKeys]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || creating) return;
    setCreating(true);
    try {
      const created = await createAPIKey(name.trim());
      setNewKey(created);
      setName('');
      await loadKeys();
    } catch {
      // ignore
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await deleteAPIKey(id);
      await loadKeys();
    } catch {
      // ignore
    }
  };

  const handleCopy = () => {
    if (!newKey) return;
    navigator.clipboard.writeText(newKey.key).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }).catch(() => {});
  };

  return (
    <Page>
      <HeaderBar>
        <HeaderInner>
          <Title>Settings</Title>
        </HeaderInner>
      </HeaderBar>
      <Main>
        <Section>
          <SecHeader>
            <SectionH2><Key size={14} style={{ marginRight: 6, verticalAlign: 'middle' }} /> API Keys</SectionH2>
          </SecHeader>
          <SecBody>
            <form onSubmit={handleCreate}>
              <CreateRow>
                <div style={{ flex: 1 }}>
                  <Input
                    inputSize="sm"
                    placeholder="Key name (e.g. CI/CD, monitoring)"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    disabled={creating}
                  />
                </div>
                <Button type="submit" variant="primary" size="sm" disabled={creating || !name.trim()}>
                  {creating ? 'Creating...' : 'Create Key'}
                </Button>
              </CreateRow>
            </form>

            {newKey && (
              <KeyReveal>
                <KeyRevealLabel>API Key Created — Copy it now. You won't see it again.</KeyRevealLabel>
                <KeyRevealValue>
                  <KeyCode>{newKey.key}</KeyCode>
                  <CopyBtn onClick={handleCopy}>
                    <Copy size={12} />
                    {copied ? 'Copied!' : 'Copy'}
                  </CopyBtn>
                </KeyRevealValue>
              </KeyReveal>
            )}

            {loading ? null : keys.length === 0 ? (
              <EmptyText>No API keys yet. Create one above.</EmptyText>
            ) : (
              <Table>
                <thead>
                  <tr>
                    <Th>Name</Th>
                    <Th>Prefix</Th>
                    <Th>Created</Th>
                    <Th>Last Used</Th>
                    <Th></Th>
                  </tr>
                </thead>
                <tbody>
                  {keys.map((k) => (
                    <Tr key={k.id}>
                      <Td><Mono>{k.name}</Mono></Td>
                      <Td><Mono>{k.prefix}...</Mono></Td>
                      <Td>{formatDate(k.created_at)}</Td>
                      <Td>{formatDate(k.last_used_at)}</Td>
                      <Td>
                        <DeleteBtn onClick={() => handleDelete(k.id)}>
                          <Trash2 size={12} /> Delete
                        </DeleteBtn>
                      </Td>
                    </Tr>
                  ))}
                </tbody>
              </Table>
            )}
          </SecBody>
        </Section>
      </Main>
    </Page>
  );
}
