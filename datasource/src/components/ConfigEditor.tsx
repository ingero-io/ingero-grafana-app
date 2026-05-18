import React, { ChangeEvent } from 'react';
import { InlineField, Input, SecretInput, Switch } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { IngeroDataSourceOptions, IngeroSecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<IngeroDataSourceOptions, IngeroSecureJsonData> {}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const onEndpointChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, endpoint: event.target.value },
    });
  };

  const onInsecureChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, insecureSkipVerify: event.target.checked },
    });
  };

  const onBearerChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: { bearer: event.target.value },
    });
  };

  const onResetBearer = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: { ...options.secureJsonFields, bearer: false },
      secureJsonData: { ...options.secureJsonData, bearer: '' },
    });
  };

  // The @grafana/ui Input / SecretInput / Switch components don't
  // forward arbitrary props (data-testid included) to the underlying
  // <input>; the prop is dropped silently. Wrap each control in a
  // div with data-testid so the e2e suite can target the control via
  // page.getByTestId('config-editor-endpoint').locator('input').
  return (
    <>
      <InlineField
        label="Echo endpoint"
        labelWidth={20}
        interactive
        tooltip="Base URL of the Echo HTTP API, e.g. https://echo.internal:8081. Do not include /api/v2; the backend appends paths."
      >
        <div data-testid="config-editor-endpoint">
          <Input
            id="config-editor-endpoint"
            onChange={onEndpointChange}
            value={jsonData.endpoint ?? ''}
            placeholder="https://echo.internal:8081"
            width={50}
            aria-label="Echo endpoint"
          />
        </div>
      </InlineField>
      <InlineField
        label="Bearer token"
        labelWidth={20}
        interactive
        tooltip="Bearer issued by your Echo operator. Stored in Grafana's secure store; never read back to the frontend after save."
      >
        <div data-testid="config-editor-bearer">
          <SecretInput
            required
            id="config-editor-bearer"
            isConfigured={secureJsonFields?.bearer}
            value={secureJsonData?.bearer}
            placeholder="paste the Echo bearer"
            width={50}
            onReset={onResetBearer}
            onChange={onBearerChange}
            aria-label="Bearer token"
          />
        </div>
      </InlineField>
      <InlineField
        label="Skip TLS verify"
        labelWidth={20}
        interactive
        tooltip="DEV ONLY. Disables TLS certificate verification on outbound requests. The backend refuses to honor this flag on non-loopback endpoints."
      >
        <div data-testid="config-editor-insecure">
          <Switch
            id="config-editor-insecure"
            value={jsonData.insecureSkipVerify ?? false}
            onChange={onInsecureChange}
            aria-label="Skip TLS verify"
          />
        </div>
      </InlineField>
    </>
  );
}
