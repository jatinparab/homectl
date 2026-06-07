import * as TabsPrimitive from '@radix-ui/react-tabs';
import styled from '@emotion/styled';

const Root = TabsPrimitive.Root;

const List = styled(TabsPrimitive.List)`
  display: flex;
  border-bottom: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  gap: 0;
`;

const Trigger = styled(TabsPrimitive.Trigger)`
  position: relative;
  padding: 8px 14px;
  font-size: 12px;
  font-weight: 500;
  font-family: inherit;
  color: ${({ theme }) => theme.colors.textMuted};
  background: none;
  border: none;
  cursor: pointer;
  transition: color 150ms ease;
  white-space: nowrap;

  &:hover {
    color: ${({ theme }) => theme.colors.textPrimary};
  }

  &[data-state='active'] {
    color: ${({ theme }) => theme.colors.accentPrimary};

    &::after {
      content: '';
      position: absolute;
      bottom: -1px;
      left: 0;
      right: 0;
      height: 2px;
      background: ${({ theme }) => theme.colors.accentPrimary};
      border-radius: 2px 2px 0 0;
    }
  }
`;

const Content = styled(TabsPrimitive.Content)`
  padding: 16px 0;
  outline: none;
`;

export const Tabs = Object.assign(Root, { List, Trigger, Content });
