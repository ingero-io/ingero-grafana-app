import { DataSourceInstanceSettings, CoreApp, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { IngeroQuery, IngeroDataSourceOptions, DEFAULT_QUERY, ToolsListResponse, ToolDescriptor } from './types';

/**
 * Datasource for Ingero Echo. All queries are dispatched through the
 * Go backend (datasource/pkg/plugin); the frontend never talks to
 * Echo directly so the bearer stays in the secure-store-fronted
 * backend process.
 */
export class DataSource extends DataSourceWithBackend<IngeroQuery, IngeroDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<IngeroDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<IngeroQuery> {
    return DEFAULT_QUERY;
  }

  /**
   * Apply Grafana template variables to the user-editable string
   * fields of the query. The backend is dispatcher-driven and does
   * not re-substitute, so any reference to a `$variable` must be
   * resolved here before the request leaves the browser.
   */
  applyTemplateVariables(query: IngeroQuery, scopedVars: ScopedVars): IngeroQuery {
    const tsrv = getTemplateSrv();
    return {
      ...query,
      sql: query.sql ? tsrv.replace(query.sql, scopedVars) : query.sql,
      tool: query.tool ? tsrv.replace(query.tool, scopedVars) : query.tool,
      clusterId: query.clusterId ? tsrv.replace(query.clusterId, scopedVars) : query.clusterId,
      timeWindow: query.timeWindow ? tsrv.replace(query.timeWindow, scopedVars) : query.timeWindow,
    };
  }

  /**
   * Decide whether a query should be sent to the backend at all.
   * Empty / unconfigured panels stay quiet (Grafana renders the
   * panel placeholder rather than firing a request).
   */
  filterQuery(query: IngeroQuery): boolean {
    const qt = query.queryType ?? (query.sql ? 'sql' : undefined);
    switch (qt) {
      case 'sql':
        return Boolean(query.sql && query.sql.trim());
      case 'tool':
        return Boolean(query.tool);
      case 'anomaly':
        return true;
      default:
        return false;
    }
  }

  /**
   * Fetch the cached tools/list from the backend's resource endpoint.
   * The query editor calls this to populate the tool-name picker.
   * Returns an empty list on error; the editor renders an inline
   * hint rather than blocking the user.
   */
  async fetchTools(): Promise<ToolDescriptor[]> {
    try {
      const body = await this.getResource<ToolsListResponse>('tools');
      return body.tools ?? [];
    } catch {
      return [];
    }
  }
}
