import { useMemo } from 'react';
import { ArrowRight, Shield, Key, AlertTriangle, CheckCircle } from 'lucide-react';
import type { Policy, Provider } from '@/lib/policy-types';
import {
  formatProvider,
  providerBadgeClasses,
  getConfidenceColor,
  getConfidenceLabel,
} from '@/lib/policy-types';

interface RBACMapperProps {
  policies: Policy[];
}

interface ProviderGroup {
  provider: Provider;
  policies: Policy[];
  totalRoles: number;
  avgConfidence: number;
  unmappedCount: number;
}

export function RBACMapper({ policies }: RBACMapperProps) {
  const providerGroups = useMemo(() => {
    const groups: Record<Provider, Policy[]> = {
      aws: [],
      gcp: [],
      azure: [],
    };

    // Group IAM policies by provider (exclude network policies)
    policies
      .filter((p) => p.type !== 'network' && p.keycloak_mapping)
      .forEach((policy) => {
        groups[policy.provider].push(policy);
      });

    // Calculate stats for each provider
    return (Object.entries(groups) as [Provider, Policy[]][])
      .filter(([, policies]) => policies.length > 0)
      .map(([provider, policies]) => {
        const totalRoles = policies.reduce(
          (sum, p) => sum + (p.keycloak_mapping?.roles?.length || 0),
          0
        );
        const avgConfidence =
          policies.reduce(
            (sum, p) => sum + (p.keycloak_mapping?.mapping_confidence || 0),
            0
          ) / policies.length;
        const unmappedCount = policies.reduce(
          (sum, p) => sum + (p.keycloak_mapping?.unmapped_actions?.length || 0),
          0
        );

        return {
          provider,
          policies,
          totalRoles,
          avgConfidence,
          unmappedCount,
        } as ProviderGroup;
      });
  }, [policies]);

  // Collect all generated Keycloak roles
  const keycloakRoles = useMemo(() => {
    const roles = new Map<string, { count: number; sources: string[] }>();

    policies.forEach((policy) => {
      policy.keycloak_mapping?.roles?.forEach((role) => {
        const existing = roles.get(role.name);
        if (existing) {
          existing.count++;
          existing.sources.push(`${policy.provider}/${policy.resource_name}`);
        } else {
          roles.set(role.name, {
            count: 1,
            sources: [`${policy.provider}/${policy.resource_name}`],
          });
        }
      });
    });

    return Array.from(roles.entries()).sort((a, b) => b[1].count - a[1].count);
  }, [policies]);

  if (providerGroups.length === 0) {
    return (
      <div className="empty-state">
        <Shield className="empty-state-icon" />
        <p className="empty-state-title">No RBAC Mappings</p>
        <p className="empty-state-description">
          No IAM or resource policies with Keycloak mappings found.
          Import or analyze cloud resources to generate RBAC mappings.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Overview Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <div className="card-stat">
          <p className="card-stat-label">Cloud Policies</p>
          <p className="card-stat-value">
            {policies.filter((p) => p.type !== 'network').length}
          </p>
        </div>
        <div className="card-stat">
          <p className="card-stat-label">Keycloak Roles</p>
          <p className="card-stat-value">{keycloakRoles.length}</p>
        </div>
        <div className="card-stat">
          <p className="card-stat-label">High Confidence</p>
          <p className="card-stat-value text-green-600">
            {policies.filter((p) => (p.keycloak_mapping?.mapping_confidence || 0) >= 0.8).length}
          </p>
        </div>
        <div className="card-stat">
          <p className="card-stat-label">Needs Review</p>
          <p className="card-stat-value text-yellow-600">
            {policies.filter((p) => (p.keycloak_mapping?.mapping_confidence || 0) < 0.5).length}
          </p>
        </div>
      </div>

      {/* Two-Column Mapping View */}
      <div className="grid md:grid-cols-2 gap-6">
        {/* Left Column: Cloud Policies */}
        <div className="border border-border rounded-lg p-4">
          <h3 className="font-semibold mb-4 flex items-center gap-2">
            <Shield className="h-5 w-5 text-primary" />
            Cloud IAM Policies
          </h3>

          <div className="space-y-4">
            {providerGroups.map((group) => (
              <div key={group.provider} className="border border-border rounded-lg p-3">
                <div className="flex items-center justify-between mb-3">
                  <span className={providerBadgeClasses[group.provider]}>
                    {formatProvider(group.provider)}
                  </span>
                  <span className="text-sm text-muted-foreground">
                    {group.policies.length} policies
                  </span>
                </div>

                <div className="space-y-2">
                  {group.policies.slice(0, 5).map((policy) => (
                    <div
                      key={policy.id}
                      className="flex items-center justify-between text-sm p-2 bg-muted/50 rounded"
                    >
                      <div className="flex items-center gap-2">
                        {policy.warnings && policy.warnings.length > 0 ? (
                          <AlertTriangle className="h-4 w-4 text-yellow-500" />
                        ) : (
                          <CheckCircle className="h-4 w-4 text-green-500" />
                        )}
                        <span className="truncate max-w-48" title={policy.name}>
                          {policy.name}
                        </span>
                      </div>
                      <span
                        className={`text-xs ${getConfidenceColor(
                          policy.keycloak_mapping?.mapping_confidence || 0
                        )}`}
                      >
                        {Math.round((policy.keycloak_mapping?.mapping_confidence || 0) * 100)}%
                      </span>
                    </div>
                  ))}
                  {group.policies.length > 5 && (
                    <p className="text-xs text-muted-foreground text-center">
                      +{group.policies.length - 5} more policies
                    </p>
                  )}
                </div>

                {/* Provider Summary */}
                <div className="mt-3 pt-3 border-t border-border flex justify-between text-xs text-muted-foreground">
                  <span>
                    Avg. Confidence:{' '}
                    <span className={getConfidenceColor(group.avgConfidence)}>
                      {getConfidenceLabel(group.avgConfidence)}
                    </span>
                  </span>
                  {group.unmappedCount > 0 && (
                    <span className="text-yellow-600">
                      {group.unmappedCount} unmapped actions
                    </span>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Arrow */}
        <div className="hidden md:flex items-center justify-center absolute left-1/2 transform -translate-x-1/2 top-1/2 -translate-y-1/2">
          <ArrowRight className="h-8 w-8 text-primary" />
        </div>

        {/* Right Column: Keycloak Roles */}
        <div className="border border-border rounded-lg p-4">
          <h3 className="font-semibold mb-4 flex items-center gap-2">
            <Key className="h-5 w-5 text-accent" />
            Keycloak Roles
          </h3>

          {keycloakRoles.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-8">
              No roles generated yet
            </p>
          ) : (
            <div className="space-y-2">
              {keycloakRoles.slice(0, 15).map(([roleName, data]) => (
                <div
                  key={roleName}
                  className="flex items-center justify-between p-2 bg-muted/50 rounded"
                >
                  <div className="flex items-center gap-2">
                    <div
                      className={`h-2 w-2 rounded-full ${
                        data.count >= 3
                          ? 'bg-green-500'
                          : data.count >= 2
                          ? 'bg-yellow-500'
                          : 'bg-gray-400'
                      }`}
                    />
                    <span className="text-sm font-medium">{roleName}</span>
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {data.count} source{data.count !== 1 ? 's' : ''}
                  </span>
                </div>
              ))}
              {keycloakRoles.length > 15 && (
                <p className="text-xs text-muted-foreground text-center">
                  +{keycloakRoles.length - 15} more roles
                </p>
              )}
            </div>
          )}

          {/* Legend */}
          <div className="mt-4 pt-4 border-t border-border">
            <p className="text-xs text-muted-foreground mb-2">Role Usage:</p>
            <div className="flex gap-4 text-xs">
              <span className="flex items-center gap-1">
                <div className="h-2 w-2 rounded-full bg-green-500" />
                High (3+ sources)
              </span>
              <span className="flex items-center gap-1">
                <div className="h-2 w-2 rounded-full bg-yellow-500" />
                Medium (2 sources)
              </span>
              <span className="flex items-center gap-1">
                <div className="h-2 w-2 rounded-full bg-gray-400" />
                Low (1 source)
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Unmapped Actions Warning */}
      {providerGroups.some((g) => g.unmappedCount > 0) && (
        <div className="alert-warning">
          <AlertTriangle className="h-5 w-5 flex-shrink-0" />
          <div>
            <p className="font-medium">Some actions could not be automatically mapped</p>
            <p className="text-sm mt-1">
              Review the policies with low confidence scores and manually configure Keycloak roles
              for unmapped cloud actions.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
