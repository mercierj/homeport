import { useState, useMemo } from 'react';
import { Search, Filter, X, AlertTriangle, Shield, Database, Globe } from 'lucide-react';
import { PolicyViewer } from './PolicyViewer';
import type { Policy, PolicyType, Provider, PolicyFilter } from '@/lib/policy-types';
import { formatPolicyType, formatProvider } from '@/lib/policy-types';

interface PolicyListProps {
  policies: Policy[];
  onEdit?: (policy: Policy) => void;
  onDelete?: (policy: Policy) => void;
  loading?: boolean;
}

export function PolicyList({ policies, onEdit, onDelete, loading }: PolicyListProps) {
  const [filter, setFilter] = useState<PolicyFilter>({});
  const [showFilters, setShowFilters] = useState(false);

  const filteredPolicies = useMemo(() => {
    return policies.filter((policy) => {
      // Type filter
      if (filter.types && filter.types.length > 0 && !filter.types.includes(policy.type)) {
        return false;
      }

      // Provider filter
      if (filter.providers && filter.providers.length > 0 && !filter.providers.includes(policy.provider)) {
        return false;
      }

      // Warnings filter
      if (filter.has_warnings === true && (!policy.warnings || policy.warnings.length === 0)) {
        return false;
      }
      if (filter.has_warnings === false && policy.warnings && policy.warnings.length > 0) {
        return false;
      }

      // Search filter
      if (filter.search) {
        const search = filter.search.toLowerCase();
        if (
          !policy.name.toLowerCase().includes(search) &&
          !policy.resource_name.toLowerCase().includes(search) &&
          !policy.resource_type.toLowerCase().includes(search)
        ) {
          return false;
        }
      }

      return true;
    });
  }, [policies, filter]);

  const toggleTypeFilter = (type: PolicyType) => {
    const types = filter.types || [];
    if (types.includes(type)) {
      setFilter({ ...filter, types: types.filter((t) => t !== type) });
    } else {
      setFilter({ ...filter, types: [...types, type] });
    }
  };

  const toggleProviderFilter = (provider: Provider) => {
    const providers = filter.providers || [];
    if (providers.includes(provider)) {
      setFilter({ ...filter, providers: providers.filter((p) => p !== provider) });
    } else {
      setFilter({ ...filter, providers: [...providers, provider] });
    }
  };

  const clearFilters = () => {
    setFilter({});
  };

  const hasActiveFilters = (filter.types?.length || 0) > 0 || (filter.providers?.length || 0) > 0 || filter.has_warnings !== undefined || filter.search;

  if (loading) {
    return (
      <div className="space-y-4">
        {[1, 2, 3].map((i) => (
          <div key={i} className="card-resource animate-pulse">
            <div className="flex items-center gap-3">
              <div className="skeleton h-10 w-10 rounded" />
              <div className="flex-1">
                <div className="skeleton h-4 w-48 mb-2" />
                <div className="skeleton h-3 w-32" />
              </div>
            </div>
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Search and Filter Bar */}
      <div className="flex items-center gap-4">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search policies..."
            value={filter.search || ''}
            onChange={(e) => setFilter({ ...filter, search: e.target.value })}
            className="input pl-10"
          />
        </div>
        <button
          onClick={() => setShowFilters(!showFilters)}
          className={`flex items-center gap-2 px-3 py-2 rounded ${
            hasActiveFilters ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
          }`}
        >
          <Filter className="h-4 w-4" />
          Filters
          {hasActiveFilters && (
            <span className="bg-white text-primary px-1.5 py-0.5 rounded-full text-xs">
              {(filter.types?.length || 0) + (filter.providers?.length || 0) + (filter.has_warnings !== undefined ? 1 : 0)}
            </span>
          )}
        </button>
        {hasActiveFilters && (
          <button
            onClick={clearFilters}
            className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            <X className="h-4 w-4" />
            Clear
          </button>
        )}
      </div>

      {/* Filter Panel */}
      {showFilters && (
        <div className="border border-border rounded-lg p-4 space-y-4">
          {/* Type Filters */}
          <div>
            <p className="text-sm font-medium mb-2">Policy Type</p>
            <div className="flex flex-wrap gap-2">
              {(['iam', 'resource', 'network'] as PolicyType[]).map((type) => (
                <button
                  key={type}
                  onClick={() => toggleTypeFilter(type)}
                  className={`flex items-center gap-1 px-3 py-1 rounded text-sm ${
                    filter.types?.includes(type)
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted hover:bg-muted/80'
                  }`}
                >
                  {type === 'iam' && <Shield className="h-3 w-3" />}
                  {type === 'resource' && <Database className="h-3 w-3" />}
                  {type === 'network' && <Globe className="h-3 w-3" />}
                  {formatPolicyType(type)}
                </button>
              ))}
            </div>
          </div>

          {/* Provider Filters */}
          <div>
            <p className="text-sm font-medium mb-2">Provider</p>
            <div className="flex flex-wrap gap-2">
              {(['aws', 'gcp', 'azure'] as Provider[]).map((provider) => (
                <button
                  key={provider}
                  onClick={() => toggleProviderFilter(provider)}
                  className={`px-3 py-1 rounded text-sm ${
                    filter.providers?.includes(provider)
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted hover:bg-muted/80'
                  }`}
                >
                  {formatProvider(provider)}
                </button>
              ))}
            </div>
          </div>

          {/* Warnings Filter */}
          <div>
            <p className="text-sm font-medium mb-2">Warnings</p>
            <div className="flex flex-wrap gap-2">
              <button
                onClick={() =>
                  setFilter({
                    ...filter,
                    has_warnings: filter.has_warnings === true ? undefined : true,
                  })
                }
                className={`flex items-center gap-1 px-3 py-1 rounded text-sm ${
                  filter.has_warnings === true
                    ? 'bg-yellow-500 text-white'
                    : 'bg-muted hover:bg-muted/80'
                }`}
              >
                <AlertTriangle className="h-3 w-3" />
                With Warnings
              </button>
              <button
                onClick={() =>
                  setFilter({
                    ...filter,
                    has_warnings: filter.has_warnings === false ? undefined : false,
                  })
                }
                className={`px-3 py-1 rounded text-sm ${
                  filter.has_warnings === false
                    ? 'bg-green-500 text-white'
                    : 'bg-muted hover:bg-muted/80'
                }`}
              >
                No Warnings
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Results Count */}
      <p className="text-sm text-muted-foreground">
        Showing {filteredPolicies.length} of {policies.length} policies
      </p>

      {/* Policy List */}
      {filteredPolicies.length === 0 ? (
        <div className="empty-state">
          <Shield className="empty-state-icon" />
          <p className="empty-state-title">No policies found</p>
          <p className="empty-state-description">
            {hasActiveFilters
              ? 'Try adjusting your filters or search query.'
              : 'No policies have been extracted yet.'}
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {filteredPolicies.map((policy) => (
            <PolicyViewer
              key={policy.id}
              policy={policy}
              onEdit={onEdit ? () => onEdit(policy) : undefined}
              onDelete={onDelete ? () => onDelete(policy) : undefined}
            />
          ))}
        </div>
      )}
    </div>
  );
}
