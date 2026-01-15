import { useCredentialsStore } from '@/stores/credentials';

const API_BASE = import.meta.env.DEV
  ? 'http://localhost:8080/api/v1'
  : '/api/v1';

function getDatabaseHeaders(): HeadersInit {
  const creds = useCredentialsStore.getState().database;
  if (!creds) return {};
  return {
    'X-Database-Host': creds.host,
    'X-Database-Port': String(creds.port),
    'X-Database-User': creds.user,
    'X-Database-Password': creds.password,
    'X-Database-Name': creds.database,
  };
}

export interface Database {
  name: string;
  owner: string;
  size: string;
}

export interface Table {
  schema: string;
  name: string;
  type: string;
  owner: string;
  size: string;
}

export interface Column {
  name: string;
  type: string;
  nullable: boolean;
  default?: string;
  primary_key: boolean;
}

export interface QueryResult {
  columns: string[];
  rows: unknown[][];
  row_count: number;
  duration?: string;
}

export async function listDatabases(stackId = 'default'): Promise<{ databases: Database[] }> {
  const response = await fetch(`${API_BASE}/stacks/${stackId}/database/databases`, {
    headers: getDatabaseHeaders(),
  });
  if (!response.ok) throw new Error('Failed to list databases');
  return response.json();
}

export async function listTables(stackId: string, schema = 'public'): Promise<{ tables: Table[] }> {
  const response = await fetch(
    `${API_BASE}/stacks/${stackId}/database/tables?schema=${schema}`,
    { headers: getDatabaseHeaders() }
  );
  if (!response.ok) throw new Error('Failed to list tables');
  return response.json();
}

export async function getTableSchema(
  stackId: string,
  table: string,
  schema = 'public'
): Promise<{ columns: Column[] }> {
  const response = await fetch(
    `${API_BASE}/stacks/${stackId}/database/tables/${table}/schema?schema=${schema}`,
    { headers: getDatabaseHeaders() }
  );
  if (!response.ok) throw new Error('Failed to get table schema');
  return response.json();
}

export async function getTableData(
  stackId: string,
  table: string,
  schema = 'public',
  limit = 100
): Promise<QueryResult> {
  const response = await fetch(
    `${API_BASE}/stacks/${stackId}/database/tables/${table}/data?schema=${schema}&limit=${limit}`,
    { headers: getDatabaseHeaders() }
  );
  if (!response.ok) throw new Error('Failed to get table data');
  return response.json();
}

export async function executeQuery(stackId: string, query: string): Promise<QueryResult> {
  const response = await fetch(`${API_BASE}/stacks/${stackId}/database/query`, {
    method: 'POST',
    headers: {
      ...getDatabaseHeaders(),
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ query, read_only: true }),
  });
  if (!response.ok) {
    const error = await response.text();
    throw new Error(error);
  }
  return response.json();
}
