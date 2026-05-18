import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

/**
 * Datasource configuration. Mirrors the Go `models.PluginSettings`
 * struct (datasource/pkg/models/settings.go).
 */
export interface IngeroDataSourceOptions extends DataSourceJsonData {
  /** Base URL of the Echo HTTP API, e.g. "https://echo.internal:8081". Must NOT include /api/v2. */
  endpoint?: string;
  /**
   * If true, skip TLS certificate verification on outbound requests.
   * Honored only for loopback hosts (127.0.0.1 / localhost / ::1);
   * production datasources leave it off.
   */
  insecureSkipVerify?: boolean;
}

/**
 * Fields that live in Grafana's secure store; never sent back to the
 * frontend after save. Mirrors `models.SecretPluginSettings`.
 */
export interface IngeroSecureJsonData {
  bearer?: string;
}

/**
 * Three query types supported by the datasource. The
 * backend dispatcher branches on `queryType`. An empty `queryType`
 * with a non-empty `sql` is treated as a legacy SQL query (for
 * panels that predate the discriminator).
 */
export type IngeroQueryType = 'sql' | 'tool' | 'anomaly';

export interface IngeroQuery extends DataQuery {
  queryType?: IngeroQueryType;

  /** queryType === 'sql'. */
  sql?: string;

  /**
   * queryType === 'tool'. Dotted MCP tool name, e.g.
   * "fleet.cluster.summary". Must match ^[a-z][a-z0-9_.]{1,127}$;
   * backend validates before dispatch.
   */
  tool?: string;

  /**
   * queryType === 'tool'. Object forwarded verbatim as the tool's
   * `args` body. The backend does not interpret it; the server's
   * JSON-schema validator does.
   */
  toolArgs?: Record<string, unknown>;

  /**
   * queryType === 'anomaly'. Synthesised into a
   * fleet.cluster.anomaly_list call. Validated client-side
   * (cluster_id charset, time_window charset) before the request
   * leaves the plugin.
   */
  timeWindow?: string;
  severityFilter?: 'warn' | 'error' | 'critical' | 'any';
  limit?: number;
  clusterId?: string;
}

export const DEFAULT_QUERY: Partial<IngeroQuery> = {
  queryType: 'sql',
};

/**
 * Tool descriptor returned by the backend's resource endpoint
 * `/resources/tools`, which caches Echo's /api/v2/tools/list
 * filtered for the calling bearer. Input/output schemas are passed
 * through raw.
 */
export interface ToolDescriptor {
  name: string;
  description: string;
  input_schema?: Record<string, unknown>;
  output_schema?: Record<string, unknown>;
}

export interface ToolsListResponse {
  tools: ToolDescriptor[];
}
