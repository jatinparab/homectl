import * as DialogPrimitive from '@radix-ui/react-dialog';
import styled from '@emotion/styled';

const Overlay = styled(DialogPrimitive.Overlay)`
  position: fixed;
  inset: 0;
  z-index: 100;
  background: rgba(0, 0, 0, 0.3);
  animation: overlayShow 0.15s ease;

  @keyframes overlayShow {
    from { opacity: 0; }
    to { opacity: 1; }
  }
`;

const Content = styled(DialogPrimitive.Content)`
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  z-index: 101;
  width: 90vw;
  max-width: 500px;
  max-height: 85vh;
  overflow-y: auto;
  background: ${({ theme }) => theme.colors.neutralSurface};
  border: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  border-radius: ${({ theme }) => theme.radii.lg};
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.12);
  animation: contentShow 0.15s ease;

  @keyframes contentShow {
    from { opacity: 0; transform: translate(-50%, -48%) scale(0.96); }
    to { opacity: 1; transform: translate(-50%, -50%) scale(1); }
  }
`;

const Title = styled(DialogPrimitive.Title)`
  font-size: 14px;
  font-weight: 600;
  color: ${({ theme }) => theme.colors.textPrimary};
  margin: 0;
`;

const Description = styled(DialogPrimitive.Description)`
  font-size: 12px;
  color: ${({ theme }) => theme.colors.textMuted};
  margin-top: 4px;
`;

interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title?: string;
  description?: string;
  children: React.ReactNode;
  trigger?: React.ReactNode;
}

export function Dialog({ open, onOpenChange, title, description, children, trigger }: DialogProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      {trigger && (
        <DialogPrimitive.Trigger asChild>
          {trigger}
        </DialogPrimitive.Trigger>
      )}
      <DialogPrimitive.Portal>
        <Overlay />
        <Content>
          {title && <Title>{title}</Title>}
          {description && <Description>{description}</Description>}
          <div style={{ marginTop: title || description ? 12 : 0 }}>
            {children}
          </div>
        </Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
