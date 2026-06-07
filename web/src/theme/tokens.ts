/** Design tokens — single source of truth for all visual values. */

export const colors = {
  /* Primary accent — matches favicon teal */
  accentPrimary: '#0d9488',
  accentLight: '#ccfbf1',
  accentHover: '#0f766e',
  accentSubtle: '#f0fdfa',
  accentMuted: '#99f6e4',

  /* Neutrals */
  neutralBg: '#f5f3ef',
  neutralSurface: '#ffffff',
  neutralSurfaceHover: '#faf9f7',
  neutralSurfaceAlt: '#f0ede8',
  neutralBorder: '#e2ddd7',
  neutralBorderStrong: '#c9c4bc',

  /* Text */
  textPrimary: '#1c1917',
  textSecondary: '#57534e',
  textMuted: '#a8a29e',
  textInverse: '#fafaf9',

  /* Status */
  success: '#059669',
  successBg: '#ecfdf5',
  successBorder: '#a7f3d0',
  warning: '#d97706',
  warningBg: '#fffbeb',
  warningBorder: '#fde68a',
  danger: '#dc2626',
  dangerBg: '#fef2f2',
  dangerBorder: '#fecaca',
  info: '#2563eb',
  infoBg: '#eff6ff',
  infoBorder: '#bfdbfe',

  /* Special */
  violet: '#7c3aed',
  violetBg: '#f5f3ff',
  violetBorder: '#ddd6fe',

  /* Tooltip / overlay */
  tooltipBg: '#1c1917',
  tooltipText: '#fafaf9',
} as const;

export const typography = {
  fontFamily: "'JetBrains Mono', 'Courier New', monospace",
  sizeXs: '10px',
  sizeSm: '11px',
  sizeBase: '13px',
  sizeLg: '14px',
  sizeXl: '16px',
  weightNormal: 400,
  weightMedium: 500,
  weightSemibold: 600,
  weightBold: 700,
} as const;

export const spacing = {
  xs: '2px',
  sm: '4px',
  md: '8px',
  lg: '12px',
  xl: '16px',
  '2xl': '24px',
  '3xl': '32px',
} as const;

export const radii = {
  sm: '3px',
  md: '6px',
  lg: '8px',
  xl: '12px',
  full: '9999px',
} as const;

export const shadows = {
  glow: '0 0 0 3px rgba(5, 150, 105, 0.15), 0 0 6px 1px rgba(5, 150, 105, 0.25)',
  popover: '0 4px 12px rgba(0,0,0,0.1)',
} as const;

export const transitions = {
  fast: '150ms ease',
  base: '200ms ease',
} as const;

export const zIndex = {
  sidebar: 40,
  dropdown: 50,
  overlay: 100,
  popover: 120,
  tooltip: 150,
} as const;
