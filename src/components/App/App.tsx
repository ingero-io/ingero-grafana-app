import React from 'react';
import { css } from '@emotion/css';
import { AppRootProps, GrafanaTheme2 } from '@grafana/data';
import { useStyles2 } from '@grafana/ui';
import pluginJson from '../../plugin.json';

function App(_props: AppRootProps) {
  const s = useStyles2(getStyles);
  return (
    <div className={s.container}>
      <h2>Ingero</h2>
      <p>
        Reference dashboards, alert rules, and a native datasource for Ingero
        GPU observability. NCCL straggler triage, CUDA op profiling, memcpy
        bandwidth, memory fragmentation, throttle history.
      </p>
      <p>
        Configure the connection to your Ingero Echo endpoint on the{' '}
        <a href={`/plugins/${pluginJson.id}`}>configuration page</a>.
      </p>
      <ul className={s.list}>
        <li>
          <a href="https://github.com/ingero-io/ingero-grafana-app">
            Plugin source on GitHub
          </a>
        </li>
        <li>
          <a href="https://github.com/ingero-io/ingero">Ingero agent</a>
        </li>
        <li>
          <a href="https://github.com/ingero-io/ingero-fleet">Ingero Fleet</a>
        </li>
      </ul>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  container: css`
    padding: ${theme.spacing(3)};
    max-width: 720px;
  `,
  list: css`
    margin-top: ${theme.spacing(2)};
    line-height: ${theme.typography.body.lineHeight};
  `,
});

export default App;
