import styled from '@emotion/styled';
import { useEffect, useState } from 'react';
import { useLiveState } from '../../api/live';
import { Tooltip } from '../ui/Tooltip';

const StatusContainer = styled.div`
  display: flex;
  align-items: center;
  gap: 6px;
`;

const Dot = styled.span<{ $color: string }>`
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: ${({ $color }) => $color};
  flex-shrink: 0;
`;

const Label = styled.span<{ $visible: boolean }>`
  overflow: hidden;
  white-space: nowrap;
  font-size: 10px;
  font-weight: 500;
  color: ${({ theme }) => theme.colors.textMuted};
  transition: max-width 500ms ease-in-out, opacity 500ms ease-in-out;
  max-width: ${({ $visible }) => ($visible ? '80px' : '0')};
  opacity: ${({ $visible }) => ($visible ? 1 : 0)};
`;

const Wrapper = styled.div`
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: default;
`;

export function LiveStatusDot() {
  const { connected, error } = useLiveState();
  const [showLabel, setShowLabel] = useState(true);

  useEffect(() => {
    if (connected) {
      setShowLabel(true);
      const timer = setTimeout(() => setShowLabel(false), 1500);
      return () => clearTimeout(timer);
    }
  }, [connected]);

  const statusText = connected ? 'Connected' : error ? 'Error' : 'Offline';
  const dotColor = connected ? '#059669' : error ? '#dc2626' : '#d97706';

  return (
    <StatusContainer role="status" aria-live="polite">
      <Tooltip content={statusText}>
        <Wrapper>
          <Dot $color={dotColor} />
          <Label $visible={statusText !== 'Offline' && showLabel}>{statusText}</Label>
        </Wrapper>
      </Tooltip>
    </StatusContainer>
  );
}
