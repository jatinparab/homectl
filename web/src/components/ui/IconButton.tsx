import { IconButton as RadixIconButton } from '@radix-ui/themes';
import styled from '@emotion/styled';
import type { ComponentPropsWithoutRef } from 'react';

interface IconButtonProps extends ComponentPropsWithoutRef<typeof RadixIconButton> {
  label: string;
}

const StyledIconButton = styled(RadixIconButton)`
  && {
    cursor: pointer;
  }

  &&:disabled,
  &&[data-disabled] {
    cursor: not-allowed;
  }
`;

export function IconButton({ label, ...props }: IconButtonProps) {
  return <StyledIconButton aria-label={label} title={label} radius="small" {...props} />;
}
