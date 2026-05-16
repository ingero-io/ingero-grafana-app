import React, { ChangeEvent, useEffect, useState } from 'react';
import { InlineField, Input, Select, Stack, TextArea } from '@grafana/ui';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { DataSource } from '../datasource';
import { IngeroDataSourceOptions, IngeroQuery, IngeroQueryType, ToolDescriptor } from '../types';

type Props = QueryEditorProps<DataSource, IngeroQuery, IngeroDataSourceOptions>;

const QUERY_TYPES: Array<SelectableValue<IngeroQueryType>> = [
  { label: 'SQL', value: 'sql', description: 'Run a read-only DuckDB query against Echo via POST /api/v1/sql.' },
  { label: 'MCP tool', value: 'tool', description: 'Invoke a server-registered MCP tool via POST /api/v1/tools/<name>.' },
  { label: 'Anomaly stream', value: 'anomaly', description: 'Recent anomalies via fleet.cluster.anomaly_list.' },
];

const SEVERITY_OPTIONS: Array<SelectableValue<'warn' | 'error' | 'critical' | 'any'>> = [
  { label: 'Any', value: 'any' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' },
  { label: 'Critical', value: 'critical' },
];

// Client-side mirrors of the backend regexes. Catching bad input here
// avoids a round-trip to the backend for an obvious typo. The
// authoritative validation still lives on the backend.
const CLUSTER_ID_RE = /^[A-Za-z0-9_.-]{1,64}$/;
const TIME_WINDOW_RE = /^[0-9hdms]{1,16}$/;
const TOOL_NAME_RE = /^[a-z][a-z0-9_.]{1,127}$/;

export function QueryEditor({ query, onChange, onRunQuery, datasource }: Props) {
  const qt: IngeroQueryType = query.queryType ?? (query.sql ? 'sql' : 'sql');

  // Tool list, fetched lazily on first render of the tool / anomaly
  // editor. Empty on error or before fetch; the picker shows a hint
  // until the result lands. Only the post-fetch setter fires inside
  // the effect (no synchronous-set-in-effect anti-pattern).
  type ToolsFetch = { status: 'idle' | 'ready'; tools: ToolDescriptor[] };
  const [toolsState, setToolsState] = useState<ToolsFetch>({ status: 'idle', tools: [] });
  useEffect(() => {
    if (qt !== 'tool' && qt !== 'anomaly') {
      return;
    }
    let cancelled = false;
    datasource.fetchTools().then((t) => {
      if (!cancelled) {
        setToolsState({ status: 'ready', tools: t });
      }
    });
    return () => {
      cancelled = true;
    };
  }, [qt, datasource]);
  const tools = toolsState.tools;
  const toolsLoading = toolsState.status === 'idle' && (qt === 'tool' || qt === 'anomaly');

  const onQueryTypeChange = (s: SelectableValue<IngeroQueryType>) => {
    onChange({ ...query, queryType: s.value ?? 'sql' });
  };
  const onSqlChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    onChange({ ...query, sql: event.target.value });
  };
  const onToolChange = (s: SelectableValue<string>) => {
    onChange({ ...query, tool: s.value, toolArgs: {} });
  };
  const onToolArgsChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    let parsed: Record<string, unknown> = {};
    const trimmed = event.target.value.trim();
    if (trimmed) {
      try {
        parsed = JSON.parse(trimmed);
      } catch {
        // Keep the raw value as toolArgs.__raw so the user can edit
        // their typo without losing keystrokes. The backend will
        // reject invalid JSON anyway.
        onChange({ ...query, toolArgs: { __raw: event.target.value } as Record<string, unknown> });
        return;
      }
    }
    onChange({ ...query, toolArgs: parsed });
  };
  const onTimeWindowChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, timeWindow: event.target.value });
  };
  const onSeverityChange = (s: SelectableValue<'warn' | 'error' | 'critical' | 'any'>) => {
    onChange({ ...query, severityFilter: s.value });
  };
  const onLimitChange = (event: ChangeEvent<HTMLInputElement>) => {
    const n = parseInt(event.target.value, 10);
    onChange({ ...query, limit: Number.isFinite(n) && n > 0 ? n : undefined });
  };
  const onClusterIdChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, clusterId: event.target.value });
  };

  const toolOptions: Array<SelectableValue<string>> = tools.map((t) => ({
    label: t.name,
    value: t.name,
    description: t.description,
  }));

  const clusterIdInvalid = Boolean(query.clusterId && !CLUSTER_ID_RE.test(query.clusterId));
  const timeWindowInvalid = Boolean(query.timeWindow && !TIME_WINDOW_RE.test(query.timeWindow));
  const toolNameInvalid = Boolean(query.tool && !TOOL_NAME_RE.test(query.tool));

  const renderArgsBlob = (): string => {
    const a = query.toolArgs;
    if (!a) {
      return '';
    }
    if (typeof (a as { __raw?: string }).__raw === 'string') {
      return (a as { __raw: string }).__raw;
    }
    return JSON.stringify(a, null, 2);
  };

  return (
    <Stack direction="column" gap={1}>
      <InlineField label="Query type" labelWidth={20}>
        <div data-testid="query-editor-type">
          <Select
            width={30}
            value={QUERY_TYPES.find((o) => o.value === qt)}
            options={QUERY_TYPES}
            onChange={onQueryTypeChange}
          />
        </div>
      </InlineField>

      {qt === 'sql' && (
        <InlineField
          label="SQL"
          labelWidth={20}
          interactive
          tooltip="Read-only DuckDB query. Echo enforces read-only + 60s timeout + 1GB memory cap + no filesystem builtins."
        >
          <div data-testid="query-editor-sql">
            <TextArea
              id="query-editor-sql"
              value={query.sql ?? ''}
              onChange={onSqlChange}
              onBlur={() => onRunQuery()}
              placeholder="SELECT * FROM events WHERE ts > now() - INTERVAL '1' HOUR LIMIT 100"
              rows={6}
              cols={80}
              aria-label="SQL"
            />
          </div>
        </InlineField>
      )}

      {qt === 'tool' && (
        <>
          <InlineField label="Tool" labelWidth={20} invalid={toolNameInvalid} error="Tool name must match ^[a-z][a-z0-9_.]{1,127}$">
            <div data-testid="query-editor-tool">
              <Select
                width={40}
                value={toolOptions.find((o) => o.value === query.tool) ?? null}
                options={toolOptions}
                onChange={onToolChange}
                placeholder={toolsLoading ? 'loading…' : tools.length === 0 ? 'no tools (check connection)' : 'pick a tool'}
                isClearable
                allowCustomValue
                onCreateOption={(v: string) => onToolChange({ value: v, label: v })}
              />
            </div>
          </InlineField>
          <InlineField
            label="Args (JSON)"
            labelWidth={20}
            interactive
            tooltip="Forwarded verbatim as the tool's args body. The server validates against the tool's input_schema."
          >
            <div data-testid="query-editor-tool-args">
              <TextArea
                id="query-editor-tool-args"
                value={renderArgsBlob()}
                onChange={onToolArgsChange}
                onBlur={() => onRunQuery()}
                placeholder='{}'
                rows={6}
                cols={80}
                aria-label="Args (JSON)"
              />
            </div>
          </InlineField>
        </>
      )}

      {qt === 'anomaly' && (
        <>
          <InlineField
            label="Time window"
            labelWidth={20}
            invalid={timeWindowInvalid}
            error="Must match ^[0-9hdms]{1,16}$ (e.g. 1h, 24h, 7d)"
          >
            <div data-testid="query-editor-time-window">
              <Input
                id="query-editor-time-window"
                value={query.timeWindow ?? ''}
                onChange={onTimeWindowChange}
                placeholder="1h"
                width={20}
                aria-label="Time window"
              />
            </div>
          </InlineField>
          <InlineField label="Severity" labelWidth={20}>
            <Select
              width={20}
              value={SEVERITY_OPTIONS.find((o) => o.value === (query.severityFilter ?? 'any'))}
              options={SEVERITY_OPTIONS}
              onChange={onSeverityChange}
            />
          </InlineField>
          <InlineField label="Limit" labelWidth={20}>
            <Input
              id="query-editor-limit"
              type="number"
              value={query.limit ?? ''}
              onChange={onLimitChange}
              placeholder="100"
              width={20}
            />
          </InlineField>
          <InlineField
            label="Cluster ID"
            labelWidth={20}
            interactive
            tooltip="Optional. Forwarded as cluster_id; if omitted the server applies the bearer's default scope."
            invalid={clusterIdInvalid}
            error="Must match ^[A-Za-z0-9_.-]{1,64}$"
          >
            <Input
              id="query-editor-cluster-id"
              value={query.clusterId ?? ''}
              onChange={onClusterIdChange}
              placeholder="prod-a"
              width={30}
            />
          </InlineField>
        </>
      )}
    </Stack>
  );
}
