import { BaseLogPanel } from "./BaseLogPanel";

interface LogPanelProps {
  isOpen: boolean;
  onClose: () => void;
  node: string;
  appId: string;
}

export function LogPanel({ isOpen, onClose, node, appId }: LogPanelProps) {
  const eventSourceUrl = node && appId ? `/api/v1/apps/${appId}/logs?node=${node}` : null;
  return (
    <BaseLogPanel
      isOpen={isOpen}
      onClose={onClose}
      title={`${appId} (${node})`}
      eventSourceUrl={eventSourceUrl}
    />
  );
}
