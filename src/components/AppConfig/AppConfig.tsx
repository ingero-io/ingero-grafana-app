import React, { ChangeEvent, useState } from 'react';
import { lastValueFrom } from 'rxjs';
import { css } from '@emotion/css';
import {
  AppPluginMeta,
  GrafanaTheme2,
  PluginConfigPageProps,
  PluginMeta,
} from '@grafana/data';
import { DataSourcePicker, getBackendSrv } from '@grafana/runtime';
import {
  Alert,
  Button,
  Field,
  FieldSet,
  Input,
  SecretInput,
  useStyles2,
} from '@grafana/ui';
import { testIds } from '../testIds';
import type { AppPluginSettings, SecureSettings } from '../../types';

type State = {
  echoEndpoint: string;
  echoBearer: string;
  isEchoBearerSet: boolean;
  promDatasourceUid: string;
  testStatus: 'idle' | 'pending' | 'ok' | 'fail';
  testMessage: string;
};

export interface AppConfigProps
  extends PluginConfigPageProps<AppPluginMeta<AppPluginSettings>> {}

const AppConfig = ({ plugin }: AppConfigProps) => {
  const s = useStyles2(getStyles);
  const { enabled, pinned, jsonData, secureJsonFields } = plugin.meta;
  const [state, setState] = useState<State>({
    echoEndpoint: jsonData?.echoEndpoint || '',
    echoBearer: '',
    isEchoBearerSet: Boolean(secureJsonFields?.echoBearer),
    promDatasourceUid: jsonData?.promDatasourceUid || '',
    testStatus: 'idle',
    testMessage: '',
  });

  const isSubmitDisabled = !state.echoEndpoint;

  const onResetEchoBearer = () =>
    setState({
      ...state,
      echoBearer: '',
      isEchoBearerSet: false,
      testStatus: 'idle',
      testMessage: '',
    });

  const onChange = (event: ChangeEvent<HTMLInputElement>) => {
    setState({
      ...state,
      [event.target.name]: event.target.value.trim(),
      testStatus: 'idle',
      testMessage: '',
    });
  };

  const onPromDatasourceChange = (uid: string) => {
    setState({ ...state, promDatasourceUid: uid });
  };

  const canTest = Boolean(state.echoEndpoint) && Boolean(state.echoBearer);

  const onTestConnection = async () => {
    if (!canTest) {
      return;
    }
    setState({ ...state, testStatus: 'pending', testMessage: '' });
    const url = `${state.echoEndpoint.replace(/\/$/, '')}/api/v1/health`;
    try {
      const response = await lastValueFrom(
        getBackendSrv().fetch<{ status?: string }>({
          url,
          method: 'GET',
          headers: { Authorization: `Bearer ${state.echoBearer}` },
        })
      );
      if (response.status === 200) {
        setState((prev) => ({
          ...prev,
          testStatus: 'ok',
          testMessage: 'Connected.',
        }));
      } else {
        setState((prev) => ({
          ...prev,
          testStatus: 'fail',
          testMessage: `HTTP ${response.status}`,
        }));
      }
    } catch (err: unknown) {
      const message =
        err && typeof err === 'object' && 'statusText' in err
          ? String((err as { statusText?: string }).statusText)
          : 'Request failed.';
      setState((prev) => ({
        ...prev,
        testStatus: 'fail',
        testMessage: message,
      }));
    }
  };

  const onSubmit = () => {
    if (isSubmitDisabled) {
      return;
    }
    updatePluginAndReload(plugin.meta.id, {
      enabled,
      pinned,
      jsonData: {
        echoEndpoint: state.echoEndpoint,
        promDatasourceUid: state.promDatasourceUid || undefined,
      },
      secureJsonData: state.isEchoBearerSet
        ? undefined
        : { echoBearer: state.echoBearer },
    });
  };

  return (
    <form onSubmit={(e) => { e.preventDefault(); onSubmit(); }}>
      <FieldSet label="Prometheus datasource">
        <Field
          label="Datasource"
          description="Used by the bundled dashboards for Ingero metrics scraped from agent and Echo (gpu_*, gpu_cuda_operation_*, gpu_nccl_*, etc.)."
        >
          <DataSourcePicker
            current={state.promDatasourceUid}
            onChange={(ds) => onPromDatasourceChange(ds.uid)}
            type="prometheus"
            noDefault
            inputId="config-prom-datasource"
          />
        </Field>
      </FieldSet>

      <FieldSet label="Ingero Echo connection" className={s.marginTop}>
        <Field
          label="Endpoint"
          description="HTTPS URL of the Ingero Echo instance the plugin will query (no trailing slash)."
        >
          <Input
            width={60}
            name="echoEndpoint"
            id="config-echo-endpoint"
            data-testid={testIds.appConfig.echoEndpoint}
            value={state.echoEndpoint}
            placeholder="https://echo.example.internal:8080"
            onChange={onChange}
          />
        </Field>

        <Field
          label="Bearer token"
          description="Bearer for Echo authentication. Stored encrypted at rest by Grafana; never readable from the frontend after save."
          className={s.marginTop}
        >
          <SecretInput
            width={60}
            id="config-echo-bearer"
            data-testid={testIds.appConfig.echoBearer}
            name="echoBearer"
            value={state.echoBearer}
            isConfigured={state.isEchoBearerSet}
            placeholder="Paste the Echo bearer issued for this Grafana"
            onChange={onChange}
            onReset={onResetEchoBearer}
          />
        </Field>

        <div className={s.marginTop}>
          <Button
            type="button"
            variant="secondary"
            onClick={onTestConnection}
            disabled={!canTest || state.testStatus === 'pending'}
            data-testid={testIds.appConfig.testConnection}
          >
            {state.testStatus === 'pending' ? 'Testing…' : 'Test connection'}
          </Button>
          {!canTest && state.isEchoBearerSet && (
            <span className={s.hint}>
              Re-enter the bearer to test; the saved bearer is not readable.
            </span>
          )}
        </div>

        {state.testStatus === 'ok' && (
          <div className={s.marginTop} data-testid={testIds.appConfig.testStatus}>
            <Alert title="Connected to Echo" severity="success" />
          </div>
        )}
        {state.testStatus === 'fail' && (
          <div className={s.marginTop} data-testid={testIds.appConfig.testStatus}>
            <Alert title={`Connection failed: ${state.testMessage}`} severity="error" />
          </div>
        )}
      </FieldSet>

      <div className={s.marginTop}>
        <Button
          type="submit"
          data-testid={testIds.appConfig.submit}
          disabled={isSubmitDisabled}
        >
          Save settings
        </Button>
      </div>
    </form>
  );
};

export default AppConfig;

const getStyles = (theme: GrafanaTheme2) => ({
  marginTop: css`
    margin-top: ${theme.spacing(3)};
  `,
  hint: css`
    margin-left: ${theme.spacing(2)};
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
});

const updatePluginAndReload = async (
  pluginId: string,
  data: Partial<PluginMeta<AppPluginSettings>> & {
    secureJsonData?: SecureSettings;
  }
) => {
  try {
    await updatePlugin(pluginId, data);
    window.location.reload();
  } catch (e) {
    console.error('Error while updating the plugin', e);
  }
};

const updatePlugin = async (
  pluginId: string,
  data: Partial<PluginMeta> & { secureJsonData?: SecureSettings }
) => {
  const response = await getBackendSrv().fetch({
    url: `/api/plugins/${pluginId}/settings`,
    method: 'POST',
    data,
  });
  return lastValueFrom(response);
};
