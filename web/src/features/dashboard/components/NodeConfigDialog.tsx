import { useEffect, useState } from 'react';
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
import { Info, LockKeyhole, ServerCog, TriangleAlert } from 'lucide-react';
import { useNodeConfig, useUpdateNodeConfig } from '../../../api/hooks';

type FieldDef = {
  key: string;
  label: string;
  description: string;
  secret?: boolean;
};

const SHARED_FIELDS: FieldDef[] = [
  { key: 'LOCAL_HA_IP', label: 'Local HA IP', description: 'Virtual IP used by keepalived on gateway-capable nodes.' },
  { key: 'CADDY_DNS_PROVIDER', label: 'DNS provider', description: 'Caddy DNS provider module name, for example cloudflare.' },
  { key: 'CADDY_ACME_EMAIL', label: 'ACME email', description: 'Email address for certificate registration and recovery.' },
  { key: 'CADDY_ACME_CA', label: 'ACME CA', description: 'Optional ACME directory URL, useful for staging certificates.' },
  { key: 'CADDY_DNS_TOKEN_ENV', label: 'DNS token env', description: 'Environment variable referenced by generated Caddyfiles.' },
  { key: 'CF_DNS_API_TOKEN', label: 'Cloudflare token', description: 'Cloudflare API token injected into Caddy at runtime.', secret: true },
  { key: 'RESTIC_REPOSITORY', label: 'Restic repository', description: 'Repository URL used for all backup and restore jobs.' },
  { key: 'B2_ACCOUNT_ID', label: 'B2 account ID', description: 'Backblaze B2 account identifier for restic.' },
  { key: 'B2_ACCOUNT_KEY', label: 'B2 account key', description: 'Backblaze B2 application key for restic.', secret: true },
  { key: 'RESTIC_PASSWORD', label: 'Restic password', description: 'Password used to unlock the restic repository.', secret: true },
];

const AGENT_FIELDS: FieldDef[] = [
  { key: 'HOMECTL_AGENT_NAME', label: 'Agent name', description: 'Manager identity for this node. Saving a new name reconnects the agent.' },
];

const Form = styled.form`
  display: flex;
  min-height: 0;
  flex-direction: column;
`;

const SectionCard = styled(Box)`
  border: 1px solid var(--gray-5);
  border-radius: var(--radius-4);
  background: color-mix(in srgb, var(--gray-1) 72%, white);
`;

const FieldRow = styled(Grid)`
  grid-template-columns: minmax(180px, 230px) minmax(0, 1fr);
  gap: 14px;
  align-items: center;

  @media (max-width: 720px) {
    grid-template-columns: 1fr;
    gap: 8px;
  }
`;

const FieldKey = styled.code`
  display: inline-block;
  margin-top: 3px;
  color: var(--gray-10);
  font-size: 11px;
  letter-spacing: -0.01em;
`;

function initValues(fields: FieldDef[], source?: Record<string, string>) {
  return Object.fromEntries(fields.map((field) => [field.key, source?.[field.key] ?? '']));
}

interface NodeConfigDialogProps {
  node: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function NodeConfigDialog({ node, open, onOpenChange }: NodeConfigDialogProps) {
  const { data, isLoading, error } = useNodeConfig(open ? node : null);
  const updateMutation = useUpdateNodeConfig();
  const [shared, setShared] = useState<Record<string, string>>(initValues(SHARED_FIELDS));
  const [agent, setAgent] = useState<Record<string, string>>(initValues(AGENT_FIELDS));

  useEffect(() => {
    if (!data) return;
    setShared(initValues(SHARED_FIELDS, data.shared));
    setAgent(initValues(AGENT_FIELDS, data.agent));
  }, [data]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (!node) return;
    await updateMutation.mutateAsync({ node, shared, agent });
    onOpenChange(false);
  }

  const setSharedField = (key: string, value: string) => setShared((current) => ({ ...current, [key]: value }));
  const setAgentField = (key: string, value: string) => setAgent((current) => ({ ...current, [key]: value }));

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Content maxWidth="760px" aria-describedby="node-config-description">
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
              </Flex>
              <Dialog.Title>{node ? `${node} settings` : 'Node settings'}</Dialog.Title>
              <Dialog.Description id="node-config-description">
                Encrypted on the manager, synced to agents in memory. Shared values apply to every node.
              </Dialog.Description>
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

          <ScrollArea type="auto" scrollbars="vertical" style={{ maxHeight: 'min(66vh, 680px)', paddingRight: 12 }}>
            {isLoading ? (
              <Callout.Root color="teal" size="1" mb="3">
                <Callout.Icon>
                  <Info size={15} />
                </Callout.Icon>
                <Callout.Text>Loading encrypted config…</Callout.Text>
              </Callout.Root>
            ) : null}

            <ConfigSection
              title="Shared values"
              badge="All agents"
              description="Secrets and runtime settings distributed to every connected node. Blank values remain unset."
              fields={SHARED_FIELDS}
              values={shared}
              onChange={setSharedField}
            />

            <Box height="4" />

            <ConfigSection
              title="Agent specific"
              badge={node ?? 'Node'}
              description="Overrides that belong only to this node."
              fields={AGENT_FIELDS}
              values={agent}
              onChange={setAgentField}
            />
          </ScrollArea>

          <Separator size="4" my="4" />

          <Flex justify="end" gap="2">
            <Dialog.Close>
              <Button type="button" variant="surface" color="gray" disabled={updateMutation.isPending}>
                Cancel
              </Button>
            </Dialog.Close>
            <Button type="submit" color="teal" disabled={!node || isLoading || updateMutation.isPending}>
              {updateMutation.isPending ? 'Saving…' : 'Save config'}
            </Button>
          </Flex>
        </Form>
      </Dialog.Content>
    </Dialog.Root>
  );
}

interface ConfigSectionProps {
  title: string;
  badge: string;
  description: string;
  fields: FieldDef[];
  values: Record<string, string>;
  onChange: (key: string, value: string) => void;
}

function ConfigSection({ title, badge, description, fields, values, onChange }: ConfigSectionProps) {
  return (
    <SectionCard p="4">
      <Flex align="start" justify="between" gap="3" mb="4">
        <Box>
          <Text as="div" size="3" weight="bold">
            {title}
          </Text>
          <Text as="p" size="2" color="gray" mt="1" style={{ marginBottom: 0 }}>
            {description}
          </Text>
        </Box>
        <Badge color="gray" variant="surface" radius="full">
          {badge}
        </Badge>
      </Flex>

      <Flex direction="column" gap="3">
        {fields.map((field) => (
          <FieldInput key={field.key} field={field} value={values[field.key] ?? ''} onChange={(value) => onChange(field.key, value)} />
        ))}
      </Flex>
    </SectionCard>
  );
}

function FieldInput({ field, value, onChange }: { field: FieldDef; value: string; onChange: (value: string) => void }) {
  const inputId = `node-config-${field.key}`;
  return (
    <FieldRow>
      <Box>
        <Flex align="center" gap="2">
          <Text as="label" htmlFor={inputId} size="2" weight="medium">
            {field.label}
          </Text>
          {field.secret ? <LockKeyhole size={13} color="var(--amber-9)" /> : null}
        </Flex>
        <FieldKey>{field.key}</FieldKey>
        <Text as="p" size="1" color="gray" mt="1" style={{ marginBottom: 0, lineHeight: 1.35 }}>
          {field.description}
        </Text>
      </Box>
      <TextField.Root
        id={inputId}
        size="2"
        radius="medium"
        type={field.secret ? 'password' : 'text'}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        autoComplete="off"
        spellCheck={false}
        style={{ width: '100%' }}
      />
    </FieldRow>
  );
}
