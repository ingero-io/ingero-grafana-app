// Project-level webpack override.
//
// This repo is an app plugin that bundles a nested datasource
// sub-plugin in the same build. The base webpack config from
// @grafana/create-plugin assumes a single plugin at the repo root,
// so two adjustments are needed for the nested datasource entry:
//
//   1. The base config's VirtualModulesPlugin creates the
//      `grafana-public-path` module under `<root>/src/node_modules/`,
//      which is in the module-resolution walk for the app entry
//      (`src/module.tsx`) but not for the nested entry
//      (`datasource/src/module.ts`). Prepending `<root>/src/node_modules`
//      to `resolve.modules` makes it reachable from every entry.
//
//   2. The base config copies only the root `src/plugin.json` into
//      `dist/`. The datasource sub-plugin's `plugin.json` (and its
//      assets) must also land in `dist/datasource/` or Grafana
//      cannot register the datasource type. A second
//      CopyWebpackPlugin handles that.
//
// This file is honored by `webpack --config webpack.config.ts` (see
// package.json scripts).
import path from 'path';
import type { Configuration } from 'webpack';
import CopyWebpackPlugin from 'copy-webpack-plugin';
import defaultConfig from './.config/webpack/webpack.config';

const config = async (env: Record<string, unknown>): Promise<Configuration> => {
  const cfg = await defaultConfig(env as never);

  // (1) Virtual public-path reachable from the datasource entry.
  const virtualModulesDir = path.resolve(process.cwd(), 'src', 'node_modules');
  const existing = cfg.resolve?.modules ?? [];
  cfg.resolve = {
    ...cfg.resolve,
    modules: [virtualModulesDir, ...existing],
  };

  // (2) Copy the datasource sub-plugin's manifest + assets. The root
  // CopyWebpackPlugin already copies the app's plugin.json from the
  // webpack context (src/). Append a SECOND CopyWebpackPlugin scoped
  // to the datasource directory so its plugin.json + img/ + any
  // README land at the canonical Grafana plugin-bundle path
  // `dist/datasource/*`.
  const datasourceCopyPlugin = new CopyWebpackPlugin({
    patterns: [
      {
        from: path.resolve(process.cwd(), 'datasource', 'src', 'plugin.json'),
        to: path.resolve(process.cwd(), 'dist', 'datasource', 'plugin.json'),
      },
      {
        from: path.resolve(process.cwd(), 'datasource', 'src', 'img'),
        to: path.resolve(process.cwd(), 'dist', 'datasource', 'img'),
        noErrorOnMissing: true,
      },
      {
        from: path.resolve(process.cwd(), 'datasource', 'README.md'),
        to: path.resolve(process.cwd(), 'dist', 'datasource', 'README.md'),
        noErrorOnMissing: true,
      },
      {
        from: path.resolve(process.cwd(), 'datasource', 'CHANGELOG.md'),
        to: path.resolve(process.cwd(), 'dist', 'datasource', 'CHANGELOG.md'),
        noErrorOnMissing: true,
      },
    ],
  });
  cfg.plugins = [...(cfg.plugins ?? []), datasourceCopyPlugin];

  return cfg;
};

export default config;
