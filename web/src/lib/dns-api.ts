import { fetchAPI } from './api';

// Record types supported by DNS
export type RecordType = 'A' | 'AAAA' | 'CNAME' | 'MX' | 'TXT' | 'NS' | 'SRV' | 'CAA' | 'PTR';

// Zone types
export type ZoneType = 'primary' | 'secondary';

// DNS Zone interface
export interface Zone {
  id: string;
  name: string;
  type: ZoneType;
  record_count: number;
  serial: number;
  refresh: number;
  retry: number;
  expire: number;
  minimum_ttl: number;
  created_at: string;
  updated_at: string;
}

// DNS Record interface
export interface Record {
  id: string;
  zone_id: string;
  name: string;
  type: RecordType;
  value: string;
  ttl: number;
  priority?: number;
  weight?: number;
  port?: number;
  created_at: string;
  updated_at: string;
}

// Validation result interface
export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
  warnings: ValidationWarning[];
}

export interface ValidationError {
  record_id?: string;
  record_name?: string;
  message: string;
  code: string;
}

export interface ValidationWarning {
  record_id?: string;
  record_name?: string;
  message: string;
  code: string;
}

// Request interfaces
export interface CreateZoneRequest {
  name: string;
  type: ZoneType;
}

export interface CreateRecordRequest {
  name: string;
  type: RecordType;
  value: string;
  ttl: number;
  priority?: number;
  weight?: number;
  port?: number;
}

export interface UpdateRecordRequest {
  name?: string;
  type?: RecordType;
  value?: string;
  ttl?: number;
  priority?: number;
  weight?: number;
  port?: number;
}

// Response interfaces
export interface ZonesResponse {
  zones: Zone[];
  count: number;
}

export interface RecordsResponse {
  records: Record[];
  count: number;
}

// Zone API functions

export async function listZones(): Promise<ZonesResponse> {
  return fetchAPI<ZonesResponse>('/dns/zones');
}

export async function getZone(zoneID: string): Promise<Zone> {
  return fetchAPI<Zone>(`/dns/zones/${encodeURIComponent(zoneID)}`);
}

export async function createZone(name: string, type: ZoneType): Promise<Zone> {
  return fetchAPI<Zone>('/dns/zones', {
    method: 'POST',
    body: JSON.stringify({ name, type }),
  });
}

export async function deleteZone(zoneID: string): Promise<{ status: string; zone_id: string }> {
  return fetchAPI<{ status: string; zone_id: string }>(`/dns/zones/${encodeURIComponent(zoneID)}`, {
    method: 'DELETE',
  });
}

// Record API functions

export async function listRecords(zoneID: string): Promise<RecordsResponse> {
  return fetchAPI<RecordsResponse>(`/dns/zones/${encodeURIComponent(zoneID)}/records`);
}

export async function getRecord(zoneID: string, recordID: string): Promise<Record> {
  return fetchAPI<Record>(
    `/dns/zones/${encodeURIComponent(zoneID)}/records/${encodeURIComponent(recordID)}`
  );
}

export async function createRecord(zoneID: string, record: CreateRecordRequest): Promise<Record> {
  return fetchAPI<Record>(`/dns/zones/${encodeURIComponent(zoneID)}/records`, {
    method: 'POST',
    body: JSON.stringify(record),
  });
}

export async function updateRecord(
  zoneID: string,
  recordID: string,
  record: UpdateRecordRequest
): Promise<Record> {
  return fetchAPI<Record>(
    `/dns/zones/${encodeURIComponent(zoneID)}/records/${encodeURIComponent(recordID)}`,
    {
      method: 'PUT',
      body: JSON.stringify(record),
    }
  );
}

export async function deleteRecord(
  zoneID: string,
  recordID: string
): Promise<{ status: string; record_id: string }> {
  return fetchAPI<{ status: string; record_id: string }>(
    `/dns/zones/${encodeURIComponent(zoneID)}/records/${encodeURIComponent(recordID)}`,
    {
      method: 'DELETE',
    }
  );
}

// Validation API

export async function validateZone(zoneID: string): Promise<ValidationResult> {
  return fetchAPI<ValidationResult>(`/dns/zones/${encodeURIComponent(zoneID)}/validate`, {
    method: 'POST',
  });
}

// Helper functions

export function getRecordTypeColor(type: RecordType): string {
  switch (type) {
    case 'A':
      return 'bg-blue-100 text-blue-800';
    case 'AAAA':
      return 'bg-indigo-100 text-indigo-800';
    case 'CNAME':
      return 'bg-purple-100 text-purple-800';
    case 'MX':
      return 'bg-orange-100 text-orange-800';
    case 'TXT':
      return 'bg-gray-100 text-gray-800';
    case 'NS':
      return 'bg-green-100 text-green-800';
    case 'SRV':
      return 'bg-yellow-100 text-yellow-800';
    case 'CAA':
      return 'bg-red-100 text-red-800';
    case 'PTR':
      return 'bg-teal-100 text-teal-800';
    default:
      return 'bg-gray-100 text-gray-800';
  }
}

export function formatTTL(ttl: number): string {
  if (ttl < 60) {
    return `${ttl}s`;
  } else if (ttl < 3600) {
    const minutes = Math.floor(ttl / 60);
    const seconds = ttl % 60;
    return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
  } else if (ttl < 86400) {
    const hours = Math.floor(ttl / 3600);
    const minutes = Math.floor((ttl % 3600) / 60);
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  } else {
    const days = Math.floor(ttl / 86400);
    const hours = Math.floor((ttl % 86400) / 3600);
    return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  }
}

export interface RecordValidation {
  valid: boolean;
  message?: string;
}

export function validateRecordValue(type: RecordType, value: string): RecordValidation {
  if (!value || value.trim() === '') {
    return { valid: false, message: 'Value is required' };
  }

  const trimmedValue = value.trim();

  switch (type) {
    case 'A': {
      const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
      if (!ipv4Regex.test(trimmedValue)) {
        return { valid: false, message: 'Invalid IPv4 address format (e.g., 192.168.1.1)' };
      }
      const parts = trimmedValue.split('.').map(Number);
      if (parts.some((part) => part > 255)) {
        return { valid: false, message: 'Each octet must be between 0 and 255' };
      }
      return { valid: true };
    }

    case 'AAAA': {
      const ipv6Regex = /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$/;
      if (!ipv6Regex.test(trimmedValue) && !trimmedValue.includes('::')) {
        return { valid: false, message: 'Invalid IPv6 address format (e.g., 2001:db8::1)' };
      }
      return { valid: true };
    }

    case 'CNAME':
    case 'NS':
    case 'PTR': {
      const hostnameRegex = /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.?$/;
      if (!hostnameRegex.test(trimmedValue)) {
        return { valid: false, message: 'Invalid hostname format' };
      }
      return { valid: true };
    }

    case 'MX': {
      const hostnameRegex = /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.?$/;
      if (!hostnameRegex.test(trimmedValue)) {
        return { valid: false, message: 'Invalid mail server hostname' };
      }
      return { valid: true };
    }

    case 'TXT': {
      if (trimmedValue.length > 255) {
        return { valid: false, message: 'TXT record value exceeds 255 characters' };
      }
      return { valid: true };
    }

    case 'SRV': {
      const hostnameRegex = /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.?$/;
      if (trimmedValue !== '.' && !hostnameRegex.test(trimmedValue)) {
        return { valid: false, message: 'Invalid target hostname' };
      }
      return { valid: true };
    }

    case 'CAA': {
      const caaRegex = /^\d+\s+(issue|issuewild|iodef)\s+.+$/i;
      if (!caaRegex.test(trimmedValue)) {
        return { valid: false, message: 'Invalid CAA format (e.g., 0 issue "letsencrypt.org")' };
      }
      return { valid: true };
    }

    default:
      return { valid: true };
  }
}

export const TTL_PRESETS = [
  { label: '1 minute', value: 60 },
  { label: '5 minutes', value: 300 },
  { label: '15 minutes', value: 900 },
  { label: '30 minutes', value: 1800 },
  { label: '1 hour', value: 3600 },
  { label: '6 hours', value: 21600 },
  { label: '12 hours', value: 43200 },
  { label: '1 day', value: 86400 },
  { label: '1 week', value: 604800 },
];

export const RECORD_TYPE_HINTS: { [key in RecordType]: string } = {
  A: 'IPv4 address (e.g., 192.168.1.1)',
  AAAA: 'IPv6 address (e.g., 2001:db8::1)',
  CNAME: 'Canonical name / alias (e.g., www.example.com)',
  MX: 'Mail server hostname (e.g., mail.example.com)',
  TXT: 'Text record (e.g., v=spf1 include:_spf.google.com ~all)',
  NS: 'Nameserver hostname (e.g., ns1.example.com)',
  SRV: 'Service target hostname (e.g., sipserver.example.com)',
  CAA: 'Certificate Authority (e.g., 0 issue "letsencrypt.org")',
  PTR: 'Pointer / reverse DNS (e.g., mail.example.com)',
};

export const RECORD_TYPES: RecordType[] = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA', 'PTR'];
