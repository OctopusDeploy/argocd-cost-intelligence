import interLatin from '@fontsource-variable/inter/files/inter-latin-wght-normal.woff2';

// The plugin uses Inter, Octopus Deploy's interface font. Argo CD's page does not load it, so the
// Latin subset of the variable font (every weight in one file) ships inside the bundle: no request
// leaves the cluster, which keeps air-gapped installs working. The font's copyright and licence
// notice is a comment inside the rule, so it travels with the font data through minification.
//
// It is registered under its own family name so it can never change how another font named Inter
// renders elsewhere on Argo CD's page.
const FAMILY = 'Cost Intel Inter';

export const FONT_FAMILY = `'${FAMILY}', Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif`;

// The unicode-range is the Latin subset's own, as published with the font.
export const FONT_FACE = `
/* Inter. Copyright 2016 The Inter Project Authors (https://github.com/rsms/inter).
   Licensed under the SIL Open Font License, Version 1.1 (https://openfontlicense.org). */
@font-face {
  font-family: '${FAMILY}';
  font-style: normal;
  font-weight: 100 900;
  font-display: swap;
  src: url(${interLatin}) format('woff2');
  unicode-range: U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329,
    U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD;
}
`;
