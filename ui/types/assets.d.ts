// Webpack inlines font files into the bundle as data: URIs (see webpack.config.js), because Argo CD
// serves an extension as a single JavaScript file. This lives outside src/ so that src/ holds no
// .d.ts files.
declare module '*.woff2' {
  const dataUri: string;
  export default dataUri;
}
