import { Check } from 'lucide-react';
import { buttonVariants } from '@/lib/button-variants';

export type HALevel = 'none' | 'basic' | 'multi' | 'cluster';

interface HAOption {
  level: HALevel;
  title: string;
  description: string;
  servers: string;
  features: string;
  basePrice: number;
}

const haOptions: HAOption[] = [
  {
    level: 'none',
    title: 'None',
    description: 'Single server deployment',
    servers: '1 server',
    features: 'No failover',
    basePrice: 0,
  },
  {
    level: 'basic',
    title: 'Basic',
    description: 'Auto-restart on failure',
    servers: '1 server',
    features: 'Auto-restart',
    basePrice: 2,
  },
  {
    level: 'multi',
    title: 'Multi-Server',
    description: 'Load balanced setup',
    servers: '2 servers',
    features: 'Load balanced',
    basePrice: 15,
  },
  {
    level: 'cluster',
    title: 'Cluster',
    description: 'Full high availability',
    servers: '3 servers',
    features: 'Full HA',
    basePrice: 30,
  },
];

interface HALevelSelectorProps {
  selectedLevel: HALevel;
  onSelect: (level: HALevel) => void;
  baseCost: number;
}

export function HALevelSelector({
  selectedLevel,
  onSelect,
  baseCost,
}: HALevelSelectorProps) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
      {haOptions.map((option) => {
        const isSelected = selectedLevel === option.level;
        const totalPrice = baseCost + option.basePrice;

        return (
          <div
            key={option.level}
            onClick={() => onSelect(option.level)}
            className={`rounded-xl border-2 p-4 cursor-pointer transition-all ${
              isSelected
                ? 'border-accent bg-accent/5 ring-2 ring-accent/20'
                : 'border-border hover:border-accent/50'
            }`}
          >
            <h4 className="font-semibold">{option.title}</h4>
            <p className="text-sm text-muted-foreground mt-1">{option.servers}</p>
            <p className="text-sm text-muted-foreground">{option.features}</p>
            <p className="text-lg font-bold mt-3">€{totalPrice}/mo</p>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onSelect(option.level);
              }}
              className={`w-full mt-3 ${
                isSelected
                  ? buttonVariants({ variant: 'success', size: 'sm' })
                  : buttonVariants({ variant: 'outline', size: 'sm' })
              }`}
            >
              {isSelected ? (
                <>
                  <Check className="h-4 w-4 mr-1" />
                  Selected
                </>
              ) : (
                'Select'
              )}
            </button>
          </div>
        );
      })}
    </div>
  );
}
