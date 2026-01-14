import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, Loader2, AlertCircle } from 'lucide-react';
import { useState } from 'react';
import {
  compareProviders,
  SUPPORTED_PROVIDERS,
  REFERENCE_PROVIDERS,
  formatCurrency,
  type Provider,
  type CompareResponse,
} from '@/lib/providers-api';
import { ProviderCard } from './ProviderCard';
import { buttonVariants } from '@/lib/button-variants';

interface ProviderComparisonProps {
  mappingResults: unknown;
  estimatedStorageGB?: number;
  estimatedEgressGB?: number;
  onSelect: (provider: Provider) => void;
  onBack: () => void;
}

export function ProviderComparison({
  mappingResults,
  estimatedStorageGB = 100,
  estimatedEgressGB = 50,
  onSelect,
  onBack,
}: ProviderComparisonProps) {
  const [selectedProvider, setSelectedProvider] = useState<Provider | null>(null);

  const { data, isLoading, error } = useQuery<CompareResponse>({
    queryKey: ['providers-compare', mappingResults, estimatedStorageGB, estimatedEgressGB],
    queryFn: () =>
      compareProviders({
        mapping_results: mappingResults,
        providers: [...SUPPORTED_PROVIDERS, ...REFERENCE_PROVIDERS],
        ha_level: 'none',
        estimated_storage_gb: estimatedStorageGB,
        estimated_egress_gb: estimatedEgressGB,
      }),
  });

  const handleSelect = (provider: Provider) => {
    setSelectedProvider(provider);
  };

  const handleConfirm = () => {
    if (selectedProvider) {
      onSelect(selectedProvider);
    }
  };

  // Filter estimates into supported and reference providers
  const supportedEstimates = data?.estimates.filter((e) =>
    SUPPORTED_PROVIDERS.includes(e.provider)
  );
  const referenceEstimates = data?.estimates.filter((e) =>
    REFERENCE_PROVIDERS.includes(e.provider)
  );

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center py-16 space-y-4">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <p className="text-muted-foreground">Calculating provider costs...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-16 space-y-4">
        <AlertCircle className="h-8 w-8 text-error" />
        <p className="text-error">Failed to load provider comparison</p>
        <p className="text-sm text-muted-foreground">{(error as Error).message}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="text-center">
        <h2 className="text-2xl font-bold mb-2">Choose Your Cloud Provider</h2>
        <p className="text-muted-foreground">
          Compare EU-based providers with GDPR compliance and competitive pricing
        </p>
      </div>

      {/* Provider Cards Grid */}
      {supportedEstimates && supportedEstimates.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {supportedEstimates.map((estimate) => (
            <ProviderCard
              key={estimate.provider}
              estimate={estimate}
              isSelected={selectedProvider === estimate.provider}
              isBestValue={data?.best_value === estimate.provider}
              onSelect={handleSelect}
            />
          ))}
        </div>
      )}

      {/* Reference Providers (not supported) */}
      {referenceEstimates && referenceEstimates.length > 0 && (
        <div className="pt-4 border-t">
          <p className="text-sm text-muted-foreground mb-3">
            Reference (not supported for deployment):
          </p>
          <div className="flex flex-wrap gap-4">
            {referenceEstimates.map((estimate) => (
              <div
                key={estimate.provider}
                className="flex items-center gap-2 text-sm text-muted-foreground"
              >
                <span
                  className={`badge ${
                    estimate.provider === 'aws'
                      ? 'badge-aws'
                      : estimate.provider === 'gcp'
                      ? 'badge-gcp'
                      : 'badge-azure'
                  }`}
                >
                  {estimate.provider.toUpperCase()}
                </span>
                <span>
                  {formatCurrency(estimate.total_monthly, estimate.currency)}/mo
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Action Buttons */}
      <div className="flex justify-between pt-4 border-t">
        <button
          onClick={onBack}
          className={buttonVariants({ variant: 'ghost', size: 'default' })}
        >
          <ArrowLeft className="h-4 w-4 mr-2" />
          Back
        </button>
        <button
          onClick={handleConfirm}
          disabled={!selectedProvider}
          className={buttonVariants({ variant: 'freedom', size: 'default' })}
        >
          Continue with {selectedProvider ? supportedEstimates?.find(e => e.provider === selectedProvider)?.display_name : 'selected provider'}
        </button>
      </div>
    </div>
  );
}
