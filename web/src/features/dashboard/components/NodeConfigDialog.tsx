import { useEffect, useMemo, useState } from 'react';
import styled from '@emotion/styled';
import {
  Badge,
  Box,
  Button,
  Callout,
  Dialog,
  Flex,
  Grid,
  ScrollArea,
  Separator,
  Text,
  TextField,
} from '@radix-ui/themes';
import { Eye, EyeOff, Info, LockKeyhole, Plus, ServerCog, Trash2, TriangleAlert } from 'lucide-react';
import { useNodeConfig, useUpdateNodeConfig } from '../../../api/hooks';
import { Tooltip } from '../../../components/ui/Tooltip';

type Scope = 'global' | 'local';

type FieldDef = {
  key: string;
  label: string;
  description: string;
  secret?: boolean;
  lockedKey?: boolean;
  lockedScope?: Scope;
};

type EnvRow = {
  id: string;
  key: string;
  value: string;
  scope: Scope;
  label?: string;
  description?: string;
  secret?: boolean;
  lockedKey?: boolean;
  lockedScope?: Scope;
};

const REQUIRED_GLOBAL_FIELDS: FieldDef[] = [
  { key: 'LOCAL_HA_IP', label: 'Local HA IP', description: 'Virtual IP used by keepalived on gateway-capable nodes.', lockedKey: true, lockedScope: 'global' },
  { key: 'CADDY_DNS_PROVIDER', label: 'DNS provider', description: 'Caddy DNS provider module name, for example cloudflare.', lockedKey: true, lockedScope: 'global' },
  { key: 'CADDY_ACME_EMAIL', label: 'ACME email', description: 'Email address for certificate registration and recovery.', lockedKey: true, lockedScope: 'global' },
  { key: 'CADDY_ACME_CA', label: 'ACME CA', description: 'Optional ACME directory URL, useful for staging certificates.', lockedKey: true, lockedScope: 'global' },
  { key: 'RESTIC_REPOSITORY', label: 'Restic repository', description: 'Repository URL used for all backup and restore jobs.', lockedKey: true, lockedScope: 'global' },
];

const AGENT_NAME_FIELD: FieldDef = {
  key: 'HOMECTL_AGENT_NAME',
  label: 'Agent name',
  description: 'Manager identity for this node. Saving a new name reconnects the agent.',
  lockedKey: true,
  lockedScope: 'local',
};

const RESTIC_PASSWORD_FIELD: FieldDef = {
  key: 'RESTIC_PASSWORD',
  label: 'Key',
  description: 'Password used to unlock the restic repository.',
  secret: true,
  lockedKey: true,
};

const EDITABLE_ENV_FIELDS: FieldDef[] = [
  { key: 'CADDY_DNS_TOKEN_ENV', label: 'Key', description: 'Environment variable referenced by generated Caddyfiles.' },
  { key: 'CF_DNS_API_TOKEN', label: 'Key', description: 'Cloudflare API token injected into Caddy at runtime.', secret: true },
  { key: 'B2_ACCOUNT_ID', label: 'Key', description: 'Backblaze B2 account identifier for restic.' },
  { key: 'B2_ACCOUNT_KEY', label: 'Key', description: 'Backblaze B2 application key for restic.', secret: true },
];

const LOCKED_KEYS = new Set([...REQUIRED_GLOBAL_FIELDS, AGENT_NAME_FIELD, RESTIC_PASSWORD_FIELD].filter((field) => field.lockedKey).map((field) => field.key));
const FIELD_DEFS = new Map([...REQUIRED_GLOBAL_FIELDS, AGENT_NAME_FIELD, RESTIC_PASSWORD_FIELD, ...EDITABLE_ENV_FIELDS].map((field) => [field.key, field]));

const Form = styled.form`
  display: flex;
  min-height: 0;
  flex-direction: column;
`;

const Panel = styled(Box)`
  border: 1px solid var(--gray-5);
  border-radius: var(--radius-3);
  background: color-mix(in srgb, var(--gray-1) 76%, white);
`;

const RowGrid = styled(Grid)`
  grid-template-columns: minmax(150px, 1.1fr) minmax(190px, 1.35fr) 136px 36px;
  gap: 10px;
  align-items: center;

  @media (max-width: 760px) {
    grid-template-columns: 1fr;
  }
`;

const HeaderGrid = styled(RowGrid)`
  padding: 0 4px;

  @media (max-width: 760px) {
    display: none;
  }
`;

const ScopeToggle = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--gray-5);
  border-radius: var(--radius-3);
  background: var(--gray-2);
`;

const ScopeButton = styled.button<{ active?: boolean }>`
  min-height: 28px;
  border: 0;
  border-radius: calc(var(--radius-3) - 2px);
  color: ${({ active }) => (active ? 'var(--teal-12)' : 'var(--gray-10)')};
  background: ${({ active }) => (active ? 'white' : 'transparent')};
  box-shadow: ${({ active }) => (active ? '0 1px 2px rgba(15, 23, 42, 0.08)' : 'none')};
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;

  &:disabled {
    color: var(--gray-8);
    cursor: not-allowed;
  }
`;

const IconAction = styled.button`
  display: inline-flex;
  width: 30px;
  height: 30px;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--gray-5);
  border-radius: var(--radius-3);
  color: var(--gray-10);
  background: white;
  cursor: pointer;

  &:disabled {
    color: var(--gray-7);
    background: var(--gray-2);
    cursor: not-allowed;
  }
`;

const ModalButton = styled(Button)`
  && {
    cursor: pointer;
  }

  &&:disabled,
  &&[data-disabled] {
    cursor: not-allowed;
  }
`;

const InlineIconButton = styled.button`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: 0;
  color: var(--gray-10);
  background: transparent;
  cursor: pointer;
`;

function rowID() {
  return `env-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function makeRow(field: FieldDef, value: string, scope: Scope): EnvRow {
  const effectiveScope = field.lockedScope ?? scope;
  return {
    id: `${field.key}-${effectiveScope}`,
    key: field.key,
    value,
    scope: effectiveScope,
    label: field.label,
    description: field.description,
    secret: field.secret,
    lockedKey: field.lockedKey,
    lockedScope: field.lockedScope,
  };
}

function buildRows(shared?: Record<string, string>, agent?: Record<string, string>, node?: string | null) {
  const rows: EnvRow[] = [];
  const usedShared = new Set<string>();
  const usedAgent = new Set<string>();

  for (const field of REQUIRED_GLOBAL_FIELDS) {
    rows.push(makeRow(field, shared?.[field.key] ?? '', 'global'));
    usedShared.add(field.key);
  }

  const resticPasswordIsLocal = agent && Object.prototype.hasOwnProperty.call(agent, RESTIC_PASSWORD_FIELD.key);
  const resticPasswordSource = resticPasswordIsLocal ? agent : shared;
  rows.push(makeRow(RESTIC_PASSWORD_FIELD, resticPasswordSource?.[RESTIC_PASSWORD_FIELD.key] ?? '', resticPasswordIsLocal ? 'local' : 'global'));
  if (resticPasswordIsLocal) usedAgent.add(RESTIC_PASSWORD_FIELD.key);
  else usedShared.add(RESTIC_PASSWORD_FIELD.key);

  rows.push(makeRow(AGENT_NAME_FIELD, agent?.[AGENT_NAME_FIELD.key] ?? node ?? '', 'local'));
  usedAgent.add(AGENT_NAME_FIELD.key);

  for (const field of EDITABLE_ENV_FIELDS) {
    const hasLocal = agent && Object.prototype.hasOwnProperty.call(agent, field.key);
    const source = hasLocal ? agent : shared;
    const scope: Scope = hasLocal ? 'local' : 'global';
    rows.push(makeRow(field, source?.[field.key] ?? '', scope));
    if (hasLocal) usedAgent.add(field.key);
    else usedShared.add(field.key);
  }

  for (const [key, value] of Object.entries(shared ?? {})) {
    if (usedShared.has(key) || LOCKED_KEYS.has(key)) continue;
    const field = FIELD_DEFS.get(key);
    rows.push(makeRow({ key, label: field?.label ?? key, description: field?.description ?? 'Custom global environment variable.', secret: field?.secret }, value, 'global'));
  }

  for (const [key, value] of Object.entries(agent ?? {})) {
    if (usedAgent.has(key) || LOCKED_KEYS.has(key)) continue;
    const field = FIELD_DEFS.get(key);
    rows.push(makeRow({ key, label: field?.label ?? key, description: field?.description ?? 'Custom local environment variable.', secret: field?.secret }, value, 'local'));
  }

  return rows;
}

function rowsToPayload(rows: EnvRow[]) {
  const shared: Record<string, string> = {};
  const agent: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    if (row.scope === 'global') shared[key] = row.value.trim();
    else agent[key] = row.value.trim();
  }
  return { shared, agent };
}

interface NodeConfigDialogProps {
  node: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function NodeConfigDialog({ node, open, onOpenChange }: NodeConfigDialogProps) {
  const { data, isLoading, error } = useNodeConfig(open ? node : null);
  const updateMutation = useUpdateNodeConfig();
  const [rows, setRows] = useState<EnvRow[]>(() => buildRows(undefined, undefined, node));

  useEffect(() => {
    if (!open) return;
    setRows(buildRows(data?.shared, data?.agent, node));
  }, [data, node, open]);

  const duplicateKeys = useMemo(() => {
    const counts = new Map<string, number>();
    rows.forEach((row) => {
      const key = row.key.trim();
      if (key) counts.set(key, (counts.get(key) ?? 0) + 1);
    });
    return new Set([...counts].filter(([, count]) => count > 1).map(([key]) => key));
  }, [rows]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (!node || duplicateKeys.size > 0) return;
    await updateMutation.mutateAsync({ node, ...rowsToPayload(rows) });
    onOpenChange(false);
  }

  function updateRow(id: string, patch: Partial<EnvRow>) {
    setRows((current) => current.map((row) => (row.id === id ? { ...row, ...patch } : row)));
  }

  function addRow() {
    setRows((current) => [...current, { id: rowID(), key: '', value: '', scope: 'local', description: 'Custom environment variable.' }]);
  }

  function removeRow(id: string) {
    setRows((current) => current.filter((row) => row.id !== id));
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Content maxWidth="860px" aria-describedby="node-config-description">
        <Form onSubmit={handleSubmit}>
          <Flex align="start" justify="between" gap="4" mb="4">
            <Box>
              <Flex align="center" gap="2" mb="2">
                <Badge color="teal" variant="soft" radius="full">
                  <ServerCog size={13} /> Node config
                </Badge>
                {data?.version ? (
                  <Badge color="gray" variant="surface" radius="full">
                    v{data.version}
                  </Badge>
                ) : null}
                <Tooltip content="Encrypted on the manager, synced to agents in memory. Global rows apply to every node; local rows apply only to this node.">
                  <span aria-label="Config sync details" role="img" style={{ display: 'inline-flex', color: 'var(--gray-9)' }}>
                    <Info size={15} />
                  </span>
                </Tooltip>
              </Flex>
              <Dialog.Title>{node ? `${node} settings` : 'Node settings'}</Dialog.Title>
              <Dialog.Description id="node-config-description">Configure manager-synced agent environment.</Dialog.Description>
            </Box>
          </Flex>

          {error ? (
            <Callout.Root color="red" size="1" mb="3">
              <Callout.Icon>
                <TriangleAlert size={15} />
              </Callout.Icon>
              <Callout.Text>{error.message}</Callout.Text>
            </Callout.Root>
          ) : null}
          {updateMutation.error ? (
            <Callout.Root color="red" size="1" mb="3">
              <Callout.Icon>
                <TriangleAlert size={15} />
              </Callout.Icon>
              <Callout.Text>{updateMutation.error.message}</Callout.Text>
            </Callout.Root>
          ) : null}
          {duplicateKeys.size > 0 ? (
            <Callout.Root color="amber" size="1" mb="3">
              <Callout.Icon>
                <TriangleAlert size={15} />
              </Callout.Icon>
              <Callout.Text>Each environment key can appear only once.</Callout.Text>
            </Callout.Root>
          ) : null}

          <ScrollArea type="auto" scrollbars="vertical" style={{ maxHeight: 'min(66vh, 680px)', paddingRight: 12 }}>
            {isLoading ? (
              <Callout.Root color="teal" size="1" mb="3">
                <Callout.Icon>
                  <Info size={15} />
                </Callout.Icon>
                <Callout.Text>Loading encrypted config...</Callout.Text>
              </Callout.Root>
            ) : null}

            <Panel p="4">
              <HeaderGrid mb="2">
                <Text size="1" color="gray" weight="bold">
                  Key
                </Text>
                <Text size="1" color="gray" weight="bold">
                  Value
                </Text>
                <Text size="1" color="gray" weight="bold">
                  Scope
                </Text>
                <span />
              </HeaderGrid>

              <Flex direction="column" gap="3">
                {rows.map((row) => (
                  <EnvInputRow
                    key={row.id}
                    row={row}
                    duplicate={duplicateKeys.has(row.key.trim())}
                    onChange={(patch) => updateRow(row.id, patch)}
                    onRemove={() => removeRow(row.id)}
                  />
                ))}
              </Flex>

              <Flex mt="4">
                <ModalButton type="button" variant="soft" color="gray" onClick={addRow}>
                  <Plus size={15} /> Add env var
                </ModalButton>
              </Flex>
            </Panel>
          </ScrollArea>

          <Separator size="4" my="4" />

          <Flex justify="end" gap="2">
            <Dialog.Close>
              <ModalButton type="button" variant="surface" color="gray" disabled={updateMutation.isPending}>
                Cancel
              </ModalButton>
            </Dialog.Close>
            <ModalButton type="submit" color="teal" disabled={!node || isLoading || updateMutation.isPending || duplicateKeys.size > 0}>
              {updateMutation.isPending ? 'Saving...' : 'Save config'}
            </ModalButton>
          </Flex>
        </Form>
      </Dialog.Content>
    </Dialog.Root>
  );
}

function EnvInputRow({
  row,
  duplicate,
  onChange,
  onRemove,
}: {
  row: EnvRow;
  duplicate: boolean;
  onChange: (patch: Partial<EnvRow>) => void;
  onRemove: () => void;
}) {
  const [visible, setVisible] = useState(false);
  const keyID = `node-config-key-${row.id}`;
  const valueID = `node-config-value-${row.id}`;
  const keyLabel = row.lockedKey && row.lockedScope ? row.label || row.key || 'Managed key' : 'Key';
  const removeLabel = row.key.trim() || 'environment variable';
  const canRemove = !row.lockedKey;

  return (
    <RowGrid>
      <Box>
        <Flex align="center" gap="2" mb="1">
          <Text as="label" htmlFor={keyID} size="1" color="gray" weight="bold">
            {keyLabel}
          </Text>
          {row.lockedKey ? <LockKeyhole size={13} color="var(--amber-9)" /> : null}
          {row.description ? (
            <Tooltip content={row.description}>
              <span aria-label={`${keyLabel} details`} role="img" style={{ display: 'inline-flex', color: 'var(--gray-9)' }}>
                <Info size={13} />
              </span>
            </Tooltip>
          ) : null}
        </Flex>
        <TextField.Root
          id={keyID}
          size="2"
          radius="medium"
          value={row.key}
          color={duplicate ? 'red' : undefined}
          readOnly={row.lockedKey}
          onChange={(event) => onChange({ key: event.target.value })}
          autoComplete="off"
          spellCheck={false}
          style={{ width: '100%' }}
        />
      </Box>

      <Box>
        <Text as="label" htmlFor={valueID} size="1" color="gray" weight="bold" mb="1" style={{ display: 'block' }}>
          Value
        </Text>
        <TextField.Root
          id={valueID}
          size="2"
          radius="medium"
          type={row.secret && !visible ? 'password' : 'text'}
          value={row.value}
          onChange={(event) => onChange({ value: event.target.value })}
          autoComplete="off"
          spellCheck={false}
          style={{ width: '100%' }}
        >
          {row.secret ? (
            <TextField.Slot side="right">
              <InlineIconButton type="button" aria-label={visible ? 'Hide value' : 'Show value'} onClick={() => setVisible((current) => !current)}>
                {visible ? <EyeOff size={15} /> : <Eye size={15} />}
              </InlineIconButton>
            </TextField.Slot>
          ) : null}
        </TextField.Root>
      </Box>

      <Box>
        <Text size="1" color="gray" weight="bold" mb="1" style={{ display: 'block' }}>
          Scope
        </Text>
        <ScopeToggle>
          <ScopeButton type="button" active={row.scope === 'global'} disabled={!!row.lockedScope} onClick={() => onChange({ scope: 'global' })}>
            Global
          </ScopeButton>
          <ScopeButton type="button" active={row.scope === 'local'} disabled={!!row.lockedScope} onClick={() => onChange({ scope: 'local' })}>
            Local
          </ScopeButton>
        </ScopeToggle>
      </Box>

      <Flex align="end" justify="end" style={{ height: '100%' }}>
        <IconAction type="button" aria-label={`Remove ${removeLabel}`} disabled={!canRemove} onClick={onRemove}>
          <Trash2 size={15} />
        </IconAction>
      </Flex>
    </RowGrid>
  );
}
