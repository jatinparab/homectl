import { css, Global } from '@emotion/react';
import { typography, colors, shadows } from './tokens';

const globalStyles = css`
  *,
  *::before,
  *::after {
    box-sizing: border-box;
  }

  html {
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
  }

  body {
    margin: 0;
    padding: 0;
    font-family: ${typography.fontFamily};
    font-size: ${typography.sizeBase};
    font-weight: ${typography.weightNormal};
    line-height: 1.5;
    color: ${colors.textPrimary};
    background-color: ${colors.neutralBg};
  }

  a {
    color: inherit;
    text-decoration: none;
  }

  button {
    font-family: inherit;
    font-size: inherit;
    cursor: pointer;
    border: none;
    background: none;
    padding: 0;
    margin: 0;
    color: inherit;
  }

  input,
  select,
  textarea {
    font-family: inherit;
    font-size: inherit;
  }

  table {
    border-collapse: collapse;
    width: 100%;
  }

  h1, h2, h3, h4, h5, h6, p {
    margin: 0;
  }

  /* Focus ring */
  :focus-visible {
    outline: 2px solid ${colors.accentPrimary};
    outline-offset: 2px;
  }

  /* Scrollbar styling */
  ::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }
  ::-webkit-scrollbar-track {
    background: transparent;
  }
  ::-webkit-scrollbar-thumb {
    background: ${colors.neutralBorderStrong};
    border-radius: 3px;
  }

  /* Selection */
  ::selection {
    background-color: ${colors.accentLight};
    color: ${colors.textPrimary};
  }

  /* Utility: health dot glow */
  .health-glow {
    box-shadow: ${shadows.glow};
  }
`;

export function GlobalStyles() {
  return <Global styles={globalStyles} />;
}
