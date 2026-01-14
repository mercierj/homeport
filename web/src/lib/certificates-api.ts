import { fetchAPI } from './api';

export interface Certificate {
  id: string;
  domain: string;
  sans?: string[];
  issuer: string;
  not_before: string;
  not_after: string;
  fingerprint: string;
  status: 'valid' | 'expiring' | 'expired' | 'pending';
  auto_renew: boolean;
  created_at: string;
  renewed_at?: string;
}

export interface CertificatesResponse {
  certificates: Certificate[];
  count: number;
}

export interface ChallengeInfo {
  domain: string;
  type: string;
  token: string;
  value: string;
}

export interface ChallengesResponse {
  challenges: ChallengeInfo[];
  count: number;
}

export interface CertificateRequest {
  domain: string;
  sans?: string[];
  auto_renew: boolean;
}

// List all managed certificates
export async function listCertificates(): Promise<CertificatesResponse> {
  return fetchAPI<CertificatesResponse>('/certificates');
}

// Get a specific certificate by domain
export async function getCertificate(domain: string): Promise<Certificate> {
  return fetchAPI<Certificate>(`/certificates/${encodeURIComponent(domain)}`);
}

// Request a new certificate
export async function requestCertificate(req: CertificateRequest): Promise<Certificate> {
  return fetchAPI<Certificate>('/certificates', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

// Renew an existing certificate
export async function renewCertificate(domain: string): Promise<Certificate> {
  return fetchAPI<Certificate>(`/certificates/${encodeURIComponent(domain)}/renew`, {
    method: 'POST',
  });
}

// Delete a certificate
export async function deleteCertificate(domain: string): Promise<{ status: string; domain: string }> {
  return fetchAPI<{ status: string; domain: string }>(`/certificates/${encodeURIComponent(domain)}`, {
    method: 'DELETE',
  });
}

// Get certificates that are expiring soon
export async function getExpiringCertificates(): Promise<CertificatesResponse> {
  return fetchAPI<CertificatesResponse>('/certificates/expiring');
}

// Get pending ACME challenges
export async function getChallenges(): Promise<ChallengesResponse> {
  return fetchAPI<ChallengesResponse>('/certificates/challenges');
}

// Trigger auto-renewal for all certificates
export async function autoRenewCertificates(): Promise<{ renewed: Certificate[]; count: number }> {
  return fetchAPI<{ renewed: Certificate[]; count: number }>('/certificates/auto-renew', {
    method: 'POST',
  });
}

// Helper function to format certificate expiry status
export function getCertificateStatusColor(status: Certificate['status']): string {
  switch (status) {
    case 'valid':
      return 'text-green-600';
    case 'expiring':
      return 'text-yellow-600';
    case 'expired':
      return 'text-red-600';
    case 'pending':
      return 'text-blue-600';
    default:
      return 'text-gray-600';
  }
}

// Helper function to format certificate expiry status badge
export function getCertificateStatusBadgeClass(status: Certificate['status']): string {
  switch (status) {
    case 'valid':
      return 'bg-green-100 text-green-800';
    case 'expiring':
      return 'bg-yellow-100 text-yellow-800';
    case 'expired':
      return 'bg-red-100 text-red-800';
    case 'pending':
      return 'bg-blue-100 text-blue-800';
    default:
      return 'bg-gray-100 text-gray-800';
  }
}

// Calculate days until expiry
export function daysUntilExpiry(notAfter: string): number {
  const expiryDate = new Date(notAfter);
  const now = new Date();
  const diffTime = expiryDate.getTime() - now.getTime();
  return Math.ceil(diffTime / (1000 * 60 * 60 * 24));
}
