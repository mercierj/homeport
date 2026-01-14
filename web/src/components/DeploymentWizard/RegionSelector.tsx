import { useQuery } from '@tanstack/react-query';
import { Globe, Loader2, AlertCircle } from 'lucide-react';
import { getProviderRegions, type Provider, type Region } from '@/lib/providers-api';

interface RegionSelectorProps {
  provider: Provider;
  selectedRegion: Region | null;
  onSelect: (region: Region) => void;
}

export function RegionSelector({ provider, selectedRegion, onSelect }: RegionSelectorProps) {
  const {
    data: regions,
    isLoading,
    isError,
    error,
  } = useQuery({
    queryKey: ['provider-regions', provider],
    queryFn: () => getProviderRegions(provider),
    enabled: !!provider,
  });

  const handleChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const regionId = e.target.value;
    if (regionId && regions) {
      const region = regions.find((r) => r.id === regionId);
      if (region) {
        onSelect(region);
      }
    }
  };

  if (isLoading) {
    return (
      <div className="space-y-2">
        <label className="label">Region</label>
        <div className="relative">
          <Globe className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <div className="skeleton h-10 w-full rounded-lg" />
          <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="space-y-2">
        <label className="label">Region</label>
        <div className="flex items-center gap-2 p-3 rounded-lg border border-red-300 bg-red-50 text-red-700">
          <AlertCircle className="h-4 w-4 flex-shrink-0" />
          <span className="text-sm">
            {error instanceof Error ? error.message : 'Failed to load regions'}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2">
      <label className="label">Region</label>
      <div className="relative">
        <Globe className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
        <select
          className="select pl-10"
          value={selectedRegion?.id || ''}
          onChange={handleChange}
        >
          <option value="">Select a region...</option>
          {regions?.map((r) => (
            <option key={r.id} value={r.id} disabled={!r.available}>
              {r.name} ({r.location}){!r.available ? ' - Unavailable' : ''}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}
