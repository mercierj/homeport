import { Check } from 'lucide-react';
import type { ProviderCostEstimate, Provider } from '@/lib/providers-api';
import { formatCurrency, formatPercentage } from '@/lib/providers-api';
import { buttonVariants } from '@/lib/button-variants';

interface ProviderCardProps {
  estimate: ProviderCostEstimate;
  isSelected: boolean;
  isBestValue: boolean;
  onSelect: (provider: Provider) => void;
}

export function ProviderCard({
  estimate,
  isSelected,
  isBestValue,
  onSelect,
}: ProviderCardProps) {
  const hasSavings = estimate.savings_percentage < 0;

  return (
    <div
      className={`relative flex flex-col rounded-xl border-2 transition-all ${
        isSelected
          ? 'border-accent bg-accent/5 ring-2 ring-accent/20'
          : 'border-border hover:border-accent/50'
      }`}
    >
      {/* Best Value Badge */}
      {isBestValue && (
        <div className="absolute -top-3 left-1/2 -translate-x-1/2">
          <span className="badge-freedom px-3 py-1 text-xs font-medium">
            Best Value
          </span>
        </div>
      )}

      {/* Header */}
      <div className="flex flex-col items-center gap-2 border-b p-4 pt-6">
        <h3 className="text-lg font-semibold">{estimate.display_name}</h3>
        {estimate.is_eu && (
          <span className="text-sm text-muted-foreground">
            <span className="mr-1">🇪🇺</span>EU
          </span>
        )}
      </div>

      {/* Cost Breakdown */}
      <div className="flex-1 p-4 space-y-3">
        <div className="flex justify-between text-sm">
          <span className="text-muted-foreground">Compute</span>
          <span className="font-medium">
            {formatCurrency(estimate.breakdown.compute_cost, estimate.currency)}
          </span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-muted-foreground">Storage</span>
          <span className="font-medium">
            {formatCurrency(estimate.breakdown.storage_cost, estimate.currency)}
          </span>
        </div>
        <div className="flex justify-between text-sm">
          <span className="text-muted-foreground">Network</span>
          <span className="font-medium">
            {formatCurrency(estimate.breakdown.network_cost, estimate.currency)}
          </span>
        </div>

        <div className="border-t pt-3">
          <div className="flex justify-between">
            <span className="font-medium">Total/mo</span>
            <span className="text-lg font-bold">
              {formatCurrency(estimate.total_monthly, estimate.currency)}
            </span>
          </div>
        </div>

        {/* Savings */}
        {hasSavings && (
          <div className="flex justify-between text-sm pt-2 border-t">
            <span className="text-muted-foreground">vs current</span>
            <span className="font-medium text-accent">
              {formatPercentage(estimate.savings_percentage)}
            </span>
          </div>
        )}
      </div>

      {/* Select Button */}
      <div className="p-4 pt-0">
        <button
          onClick={() => onSelect(estimate.provider)}
          className={`w-full ${
            isSelected
              ? buttonVariants({ variant: 'success', size: 'default' })
              : buttonVariants({ variant: 'outline', size: 'default' })
          }`}
        >
          {isSelected ? (
            <>
              <Check className="h-4 w-4 mr-2" />
              Selected
            </>
          ) : (
            'Select'
          )}
        </button>
      </div>
    </div>
  );
}
