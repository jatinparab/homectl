import * as DialogPrimitive from '@radix-ui/react-dialog';
import styled from '@emotion/styled';
import { IconButton } from './IconButton';

const Overlay = styled(DialogPrimitive.Overlay)`
  position: fixed;
  inset: 0;
  z-index: 100;
  background: rgba(0, 0, 0, 0.2);
  animation: overlayShow 0.15s ease;
  @keyframes overlayShow { from { opacity: 0; } to { opacity: 1; } }
`;

const Content = styled(DialogPrimitive.Content)`
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  z-index: 101;
  width: min(480px, 90vw);
  background: ${({ theme }) => theme.colors.neutralSurface};
  border-left: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  box-shadow: -4px 0 16px rgba(0, 0, 0, 0.08);
  display: flex;
  flex-direction: column;
  animation: slideIn 0.2s ease;
  @keyframes slideIn {
    from { transform: translateX(100%); }
    to { transform: translateX(0); }
  }
`;

const Header = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid ${({ theme }) => theme.colors.neutralBorder};

  h2 {
    font-size: 13px;
    font-weight: 600;
    color: ${({ theme }) => theme.colors.textPrimary};
    margin: 0;
  }
`;

const Body = styled.div`
  flex: 1;
  overflow-y: auto;
  padding: 16px;
`;

interface SheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title?: string;
  children: React.ReactNode;
}

export function Sheet({ open, onOpenChange, title, children }: SheetProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <Overlay />
        <Content>
          <Header>
            <h2>{title}</h2>
            <DialogPrimitive.Close asChild>
              <IconButton label="Close" variant="ghost" color="gray" size="1"><span>✕</span></IconButton>
            </DialogPrimitive.Close>
          </Header>
          <Body>{children}</Body>
        </Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
