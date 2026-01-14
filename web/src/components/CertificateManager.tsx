import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import {
  RefreshCw,
  Plus,
  Trash2,
  RotateCcw,
  Loader2,
  Shield,
  ShieldCheck,
  ShieldAlert,
  ShieldX,
  Clock,
  X,
} from 'lucide-react';
import {
  listCertificates,
  requestCertificate,
  renewCertificate,
  deleteCertificate,
  autoRenewCertificates,
  getCertificateStatusBadgeClass,
  daysUntilExpiry,
  type Certificate,
  type CertificateRequest,
} from '@/lib/certificates-api';

interface CertificateManagerProps {
  onError?: (error: Error) => void;
}

const statusIcons: Record<Certificate['status'], typeof Shield> = {
  valid: ShieldCheck,
  expiring: ShieldAlert,
  expired: ShieldX,
  pending: Shield,
};

export function CertificateManager({ onError }: CertificateManagerProps) {
  const queryClient = useQueryClient();
  const [showNewCertForm, setShowNewCertForm] = useState(false);
  const [newCertDomain, setNewCertDomain] = useState('');
  const [newCertSANs, setNewCertSANs] = useState('');
  const [newCertAutoRenew, setNewCertAutoRenew] = useState(true);
  const [activeAction, setActiveAction] = useState<{ domain: string; action: string } | null>(null);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['certificates'],
    queryFn: listCertificates,
    refetchInterval: 30000, // Refresh every 30 seconds
    refetchIntervalInBackground: false,
  });

  const clearActiveAction = () => setActiveAction(null);

  const requestMutation = useMutation({
    mutationFn: (req: CertificateRequest) => requestCertificate(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] });
      setShowNewCertForm(false);
      setNewCertDomain('');
      setNewCertSANs('');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const renewMutation = useMutation({
    mutationFn: (domain: string) => renewCertificate(domain),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (domain: string) => deleteCertificate(domain),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const autoRenewMutation = useMutation({
    mutationFn: autoRenewCertificates,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] });
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const handleSubmitNewCert = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newCertDomain.trim()) return;

    const sans = newCertSANs
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

    requestMutation.mutate({
      domain: newCertDomain.trim(),
      sans: sans.length > 0 ? sans : undefined,
      auto_renew: newCertAutoRenew,
    });
  };

  if (isLoading) {
    return (
      <div className="space-y-4 rounded-lg border p-4">
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
        </div>
        <div className="space-y-2">
          {[1, 2].map((i) => (
            <div key={i} className="flex items-center justify-between p-4 rounded-lg border">
              <div className="flex items-center gap-4">
                <Skeleton className="h-8 w-8 rounded" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-32" />
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Skeleton className="h-8 w-8" />
                <Skeleton className="h-8 w-8" />
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="card p-4">
        <div className="text-error">
          Error loading certificates. Certificate manager may not be configured.
        </div>
      </div>
    );
  }

  const certificates = data?.certificates || [];

  return (
    <div className="card p-4 space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <ShieldCheck className="h-5 w-5 text-success" />
          <h2 className="text-lg font-semibold">SSL Certificates ({certificates.length})</h2>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
          {certificates.some((c) => c.status === 'expiring') && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => autoRenewMutation.mutate()}
              disabled={autoRenewMutation.isPending}
            >
              {autoRenewMutation.isPending ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <RotateCcw className="h-4 w-4 mr-2" />
              )}
              Auto-Renew All
            </Button>
          )}
          <Button size="sm" onClick={() => setShowNewCertForm(true)}>
            <Plus className="h-4 w-4 mr-2" />
            New Certificate
          </Button>
        </div>
      </div>

      {/* New Certificate Form */}
      {showNewCertForm && (
        <div className="rounded-lg border p-4 bg-muted/50">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-medium">Request New Certificate</h3>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setShowNewCertForm(false)}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
          <form onSubmit={handleSubmitNewCert} className="space-y-4">
            <div>
              <label className="block text-sm font-medium mb-1">Domain *</label>
              <input
                type="text"
                value={newCertDomain}
                onChange={(e) => setNewCertDomain(e.target.value)}
                placeholder="example.com"
                className="w-full px-3 py-2 rounded-md border bg-background"
                required
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">
                Subject Alternative Names (comma-separated)
              </label>
              <input
                type="text"
                value={newCertSANs}
                onChange={(e) => setNewCertSANs(e.target.value)}
                placeholder="www.example.com, api.example.com"
                className="w-full px-3 py-2 rounded-md border bg-background"
              />
            </div>
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="autoRenew"
                checked={newCertAutoRenew}
                onChange={(e) => setNewCertAutoRenew(e.target.checked)}
                className="rounded border"
              />
              <label htmlFor="autoRenew" className="text-sm">
                Auto-renew before expiry
              </label>
            </div>
            <div className="flex items-center gap-2">
              <Button type="submit" disabled={requestMutation.isPending || !newCertDomain.trim()}>
                {requestMutation.isPending ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : (
                  <Plus className="h-4 w-4 mr-2" />
                )}
                Request Certificate
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => setShowNewCertForm(false)}
              >
                Cancel
              </Button>
            </div>
            {requestMutation.error && (
              <p className="text-sm text-error">
                {(requestMutation.error as Error).message}
              </p>
            )}
          </form>
        </div>
      )}

      {/* Certificate List */}
      {certificates.length === 0 ? (
        <div className="empty-state border rounded-lg">
          <Shield className="empty-state-icon" />
          <p className="empty-state-title">No certificates found</p>
          <p className="empty-state-description">Click "New Certificate" to request a Let's Encrypt certificate</p>
        </div>
      ) : (
        <div className="space-y-2">
          {certificates.map((cert) => {
            const StatusIcon = statusIcons[cert.status];
            const days = daysUntilExpiry(cert.not_after);

            return (
              <div
                key={cert.id}
                className="card-resource"
              >
                <div className="flex items-center gap-4 flex-wrap">
                  <StatusIcon
                    className={cn(
                      'h-6 w-6',
                      cert.status === 'valid' && 'text-success',
                      cert.status === 'expiring' && 'text-warning',
                      cert.status === 'expired' && 'text-error',
                      cert.status === 'pending' && 'text-info'
                    )}
                  />
                  <div>
                    <p className="font-medium">{cert.domain}</p>
                    {cert.sans && cert.sans.length > 0 && (
                      <p className="text-sm text-muted-foreground">
                        SANs: {cert.sans.join(', ')}
                      </p>
                    )}
                    <div className="flex items-center gap-2 text-sm text-muted-foreground mt-1">
                      <span
                        className={cn(
                          'px-2 py-0.5 rounded text-xs font-medium',
                          getCertificateStatusBadgeClass(cert.status)
                        )}
                      >
                        {cert.status}
                      </span>
                      <span className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {days > 0 ? `${days} days left` : 'Expired'}
                      </span>
                      {cert.auto_renew && (
                        <span className="text-success text-xs">Auto-renew</span>
                      )}
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setActiveAction({ domain: cert.domain, action: 'renew' });
                      renewMutation.mutate(cert.domain);
                    }}
                    disabled={renewMutation.isPending || deleteMutation.isPending}
                    title="Renew certificate"
                  >
                    {activeAction?.domain === cert.domain && activeAction.action === 'renew' ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <RotateCcw className="h-4 w-4" />
                    )}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      if (confirm(`Delete certificate for ${cert.domain}?`)) {
                        setActiveAction({ domain: cert.domain, action: 'delete' });
                        deleteMutation.mutate(cert.domain);
                      }
                    }}
                    disabled={deleteMutation.isPending || renewMutation.isPending}
                    title="Delete certificate"
                  >
                    {activeAction?.domain === cert.domain && activeAction.action === 'delete' ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Trash2 className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Issuer Info */}
      {certificates.length > 0 && (
        <div className="text-xs text-muted-foreground text-center pt-2">
          Certificates issued by: {certificates[0]?.issuer || "Let's Encrypt"}
        </div>
      )}
    </div>
  );
}
