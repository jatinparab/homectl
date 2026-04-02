import { useEffect, useState } from "react";
import { BaseLogPanel } from "./BaseLogPanel";

interface ContainerLogPanelProps {
  isOpen: boolean;
  onClose: () => void;
  node: string;
  containerId: string;
  containerName: string;
}

export function ContainerLogPanel({
  isOpen,
  onClose,
  node,
  containerId,
  containerName,
}: ContainerLogPanelProps) {
  const [showAll, setShowAll] = useState(false);

  useEffect(() => {
    if (!isOpen) {
      setShowAll(false);
    }
  }, [isOpen]);

  const eventSourceUrl =
    node && containerId
      ? `/api/v1/containers/${containerId}/logs?node=${node}&all=${showAll}`
      : null;

  const controls = (
    <div className="flex items-center gap-3 px-4 py-2 border-b border-gray-700">
      <label className="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
        <input
          type="checkbox"
          checked={showAll}
          onChange={(e) => setShowAll(e.target.checked)}
          className="rounded border-gray-600"
        />
        Show all logs (including restarts)
      </label>
    </div>
  );

  return (
    <BaseLogPanel
      key={`${containerId}:${showAll}`}
      isOpen={isOpen}
      onClose={onClose}
      title={`${containerName} (${node})`}
      eventSourceUrl={eventSourceUrl}
      extraControls={controls}
    />
  );
}
