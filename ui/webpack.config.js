const path = require('path');

module.exports = {
  entry: './src/index.tsx',
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: 'index.js',
  },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        // Only bundled files: tests and their setup are type-checked by `npm run typecheck`,
        // and must not emit declarations next to the sources.
        use: { loader: 'ts-loader', options: { onlyCompileBundledFiles: true } },
        exclude: /node_modules/,
      },
      {
        test: /\.css$/,
        use: ['style-loader', 'css-loader'],
      },
      {
        // Argo CD serves the extension as one JavaScript file, so fonts travel inside it.
        test: /\.woff2$/,
        type: 'asset/inline',
      },
    ],
  },
  resolve: {
    extensions: ['.tsx', '.ts', '.js'],
  },
  // React comes from the globals Argo CD sets in ui/src/app/index.tsx: React 16.14 up to 3.4, React
  // 19 from 3.5. Only 3.5 sets ReactJSXRuntime; the bundle never uses it while tsconfig keeps
  // "jsx": "react".
  externals: {
    react: 'React',
    'react-dom': 'ReactDOM',
    'react/jsx-runtime': 'ReactJSXRuntime',
  },
  devServer: {
    port: 3000,
    hot: true,
  },
};
