import { Button as RadixButton } from '@radix-ui/themes';
import styled from '@emotion/styled';
import type { ComponentPropsWithoutRef } from 'react';

type ButtonVariant = 'primary' | 'secondary' | 'soft' | 'outline' | 'ghost' | 'destructive';
type ButtonSize = 'sm' | 'md';

interface ButtonProps
  extends Omit<ComponentPropsWithoutRef<typeof RadixButton>, 'variant' | 'size'> {
  variant?: ButtonVariant;
  size?: ButtonSize;
}

const variantMap: Record<
  ButtonVariant,
  Pick<ComponentPropsWithoutRef<typeof RadixButton>, 'variant'>
> = {
  primary: { variant: 'solid' },
  secondary: { variant: 'surface' },
  soft: { variant: 'soft' },
  outline: { variant: 'outline' },
  ghost: { variant: 'ghost' },
  destructive: { variant: 'solid' },
};

const sizeMap: Record<ButtonSize, ComponentPropsWithoutRef<typeof RadixButton>['size']> = {
  sm: '1',
  md: '2',
};

const StyledButton = styled(RadixButton)`
  && {
    cursor: pointer;
  }

  &&:disabled,
  &&[data-disabled] {
    cursor: not-allowed;
  }
`;

export function Button({ variant = 'secondary', size = 'md', color, ...props }: ButtonProps) {
  const resolvedColor = color ?? (variant === 'primary' ? 'teal' : variant === 'destructive' ? 'red' : 'gray');

  return (
    <StyledButton
      variant={variantMap[variant].variant}
      size={sizeMap[size]}
      color={resolvedColor}
      radius="small"
      {...props}
    />
  );
}
