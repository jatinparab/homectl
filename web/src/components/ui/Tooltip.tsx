import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import styled from '@emotion/styled';
import type { ReactNode } from 'react';

const Content = styled(TooltipPrimitive.Content)`
  z-index: 150;
  max-width: min(320px, calc(100vw - 24px));
  padding: 6px 10px;
  font-size: ${({ theme }) => theme.typography.sizeSm};
  font-family: ${({ theme }) => theme.typography.fontFamily};
  line-height: 1.5;
  color: ${({ theme }) => theme.colors.tooltipText};
  background: ${({ theme }) => theme.colors.tooltipBg};
  border-radius: ${({ theme }) => theme.radii.md};
  box-shadow: ${({ theme }) => theme.shadows.popover};
  overflow-wrap: anywhere;
  user-select: none;
`;

const Arrow = styled(TooltipPrimitive.Arrow)`
  fill: ${({ theme }) => theme.colors.tooltipBg};
`;

type TooltipSide = 'top' | 'right' | 'bottom' | 'left';
type TooltipAlign = 'start' | 'center' | 'end';

interface TooltipProps {
  content: ReactNode;
  children: ReactNode;
  delayDuration?: number;
  side?: TooltipSide;
  align?: TooltipAlign;
}

export function Tooltip({
  content,
  children,
  delayDuration = 300,
  side = 'top',
  align = 'center',
}: TooltipProps) {
  return (
    <TooltipPrimitive.Provider delayDuration={delayDuration}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <Content
            side={side}
            align={align}
            sideOffset={6}
            collisionPadding={12}
            avoidCollisions
            sticky="always"
          >
            {content}
            <Arrow width={8} height={4} />
          </Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  );
}
