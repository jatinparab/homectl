import { colors, typography, spacing, radii, shadows, transitions, zIndex } from './tokens';

export const theme = {
  colors: {
    ...colors,
    /** Alias for neutralBorder */
    border: colors.neutralBorder,
    /** Alias for neutralSurfaceAlt */
    borderLight: colors.neutralSurfaceAlt,
    /** Alias for neutralBorderStrong */
    borderStrong: colors.neutralBorderStrong,
    /** Alias for neutralSurfaceHover */
    surfaceHover: colors.neutralSurfaceHover,
  },
  typography,
  spacing,
  radii,
  shadows,
  transitions,
  zIndex,
  // Convenience aliases
  fonts: {
    mono: typography.fontFamily,
    sans: typography.fontFamily,
  },
  fontSizes: {
    xs: typography.sizeXs,
    sm: typography.sizeSm,
    base: typography.sizeBase,
    lg: typography.sizeLg,
    xl: typography.sizeXl,
  },
} as const;

export type Theme = typeof theme;
