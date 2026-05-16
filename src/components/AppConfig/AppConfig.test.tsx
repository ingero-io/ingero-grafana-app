import React from 'react';
import { render, screen } from '@testing-library/react';
import { PluginType } from '@grafana/data';
import AppConfig, { AppConfigProps } from './AppConfig';
import { testIds } from '../testIds';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  DataSourcePicker: () => <div data-testid="data-testid ac-prom-datasource" />,
  getBackendSrv: () => ({ fetch: jest.fn() }),
}));

describe('Components/AppConfig', () => {
  let props: AppConfigProps;

  beforeEach(() => {
    jest.resetAllMocks();

    props = {
      plugin: {
        meta: {
          id: 'ingero-gpu-app',
          name: 'Ingero',
          type: PluginType.app,
          enabled: true,
          jsonData: {},
        },
      },
      query: {},
    } as unknown as AppConfigProps;
  });

  test('renders the Echo endpoint and bearer fields, plus the save button', () => {
    const plugin = { meta: { ...props.plugin.meta, enabled: false } };

    // @ts-ignore - test fixture does not need full PluginConfigPageProps
    render(<AppConfig plugin={plugin} query={props.query} />);

    expect(
      screen.queryByRole('group', { name: /ingero echo connection/i })
    ).toBeInTheDocument();
    expect(screen.queryByTestId(testIds.appConfig.echoEndpoint)).toBeInTheDocument();
    expect(screen.queryByTestId(testIds.appConfig.echoBearer)).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /save settings/i })
    ).toBeInTheDocument();
  });

  test('renders the Prometheus datasource picker', () => {
    const plugin = { meta: { ...props.plugin.meta, enabled: false } };
    // @ts-ignore - test fixture does not need full PluginConfigPageProps
    render(<AppConfig plugin={plugin} query={props.query} />);

    expect(
      screen.queryByRole('group', { name: /prometheus datasource/i })
    ).toBeInTheDocument();
    expect(screen.queryByTestId(testIds.appConfig.promDatasource)).toBeInTheDocument();
  });

  test('test-connection button is disabled until both endpoint and bearer are entered', () => {
    const plugin = { meta: { ...props.plugin.meta, enabled: false } };
    // @ts-ignore - test fixture does not need full PluginConfigPageProps
    render(<AppConfig plugin={plugin} query={props.query} />);

    const testButton = screen.getByTestId(testIds.appConfig.testConnection);
    expect(testButton).toBeDisabled();
  });
});
