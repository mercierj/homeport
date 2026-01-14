import { useState, useMemo } from 'react';
import { Globe, Cloud, Network, Info, AlertTriangle } from 'lucide-react';
import { ServiceMigrationCard } from '../ServiceMigrationCard';
import { useMigrationConfigStore } from '@/stores/migration-config';
import type { DNSConfig } from '../types';
import type { Resource } from '@/lib/migrate-api';

// Resource type matchers for filtering which cards to show
const API_GATEWAY_TYPES = ['aws_api_gateway_rest_api', 'aws_apigatewayv2_api', 'google_api_gateway_api', 'azurerm_api_management'];
const CLOUDFRONT_TYPES = ['aws_cloudfront_distribution', 'google_compute_global_address', 'azurerm_cdn_profile'];
const ROUTE53_TYPES = ['aws_route53_zone', 'google_dns_managed_zone', 'azurerm_dns_zone'];
const LB_TYPES = ['aws_lb', 'aws_alb', 'aws_elb', 'google_compute_forwarding_rule', 'azurerm_lb'];

// ============================================================================
// Helper Components
// ============================================================================

interface InfoBoxProps {
  children: React.ReactNode;
}

function InfoBox({ children }: InfoBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-info/10 border border-info/50 rounded-md">
      <Info className="w-4 h-4 text-info flex-shrink-0 mt-0.5" />
      <span className="text-sm text-info">{children}</span>
    </div>
  );
}

interface WarningBoxProps {
  children: React.ReactNode;
}

function WarningBox({ children }: WarningBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-warning/10 border border-warning/50 rounded-md">
      <AlertTriangle className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
      <span className="text-sm text-warning">{children}</span>
    </div>
  );
}

// ============================================================================
// API Gateway Configuration Types and Component
// ============================================================================

interface APIGatewayConfig {
  enabled: boolean;
  apiIds: string[];
  targetRouter: 'traefik' | 'kong';
  includeRoutes: boolean;
  includeAuthConfig: boolean;
  includeRateLimits: boolean;
  includeCorsSettings: boolean;
}

interface APIGatewayMigrationSectionProps {
  config: APIGatewayConfig;
  onConfigChange: (config: Partial<APIGatewayConfig>) => void;
}

function APIGatewayMigrationSection({ config, onConfigChange }: APIGatewayMigrationSectionProps) {
  const [apiIdsInput, setApiIdsInput] = useState(config.apiIds.join(', '));

  const handleApiIdsChange = (value: string) => {
    setApiIdsInput(value);
    const apiIds = value
      .split(',')
      .map((id) => id.trim())
      .filter((id) => id.length > 0);
    onConfigChange({ apiIds });
  };

  return (
    <div className="space-y-4">
      {/* API IDs Input */}
      <div className="space-y-1">
        <label htmlFor="api-gateway-ids" className="block text-sm font-medium text-gray-700">
          API IDs
        </label>
        <input
          id="api-gateway-ids"
          type="text"
          value={apiIdsInput}
          onChange={(e) => handleApiIdsChange(e.target.value)}
          placeholder="abc123def, xyz789ghi"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500">
          Enter API Gateway IDs separated by commas
        </p>
      </div>

      {/* Target Router Selection */}
      <div className="space-y-2">
        <label className="block text-sm font-medium text-gray-700">
          Target Router
        </label>
        <div className="flex gap-3">
          {[
            { value: 'traefik', label: 'Traefik', recommended: true },
            { value: 'kong', label: 'Kong', recommended: false },
          ].map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => onConfigChange({ targetRouter: option.value as APIGatewayConfig['targetRouter'] })}
              className={`px-4 py-2 text-sm rounded-md border transition-colors ${
                config.targetRouter === option.value
                  ? 'bg-primary/10 border-primary text-primary'
                  : 'bg-white border-input text-gray-700 hover:bg-muted'
              }`}
            >
              {option.label}
              {option.recommended && (
                <span className="ml-1 text-xs text-primary">(recommended)</span>
              )}
            </button>
          ))}
        </div>
      </div>

      {/* Options */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeRoutes}
            onChange={(e) => onConfigChange({ includeRoutes: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Routes</span>
            <p className="text-xs text-gray-500">
              Migrate API routes and path mappings
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeAuthConfig}
            onChange={(e) => onConfigChange({ includeAuthConfig: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Auth Configuration</span>
            <p className="text-xs text-gray-500">
              Migrate authentication and authorization settings
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeRateLimits}
            onChange={(e) => onConfigChange({ includeRateLimits: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Rate Limits</span>
            <p className="text-xs text-gray-500">
              Migrate throttling and rate limiting rules
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeCorsSettings}
            onChange={(e) => onConfigChange({ includeCorsSettings: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include CORS Settings</span>
            <p className="text-xs text-gray-500">
              Migrate Cross-Origin Resource Sharing configurations
            </p>
          </div>
        </label>
      </div>

      <InfoBox>
        API Gateway routes will be converted to {config.targetRouter === 'traefik' ? 'Traefik' : 'Kong'} configuration format.
        Lambda integrations will reference the containerized function endpoints.
      </InfoBox>
    </div>
  );
}

// ============================================================================
// CloudFront Configuration Types and Component
// ============================================================================

interface CloudFrontConfig {
  enabled: boolean;
  distributionIds: string[];
  includeCacheRules: boolean;
  includeOrigins: boolean;
  includeSslCertificates: boolean;
}

interface CloudFrontMigrationSectionProps {
  config: CloudFrontConfig;
  onConfigChange: (config: Partial<CloudFrontConfig>) => void;
}

function CloudFrontMigrationSection({ config, onConfigChange }: CloudFrontMigrationSectionProps) {
  const [distributionIdsInput, setDistributionIdsInput] = useState(config.distributionIds.join(', '));

  const handleDistributionIdsChange = (value: string) => {
    setDistributionIdsInput(value);
    const distributionIds = value
      .split(',')
      .map((id) => id.trim())
      .filter((id) => id.length > 0);
    onConfigChange({ distributionIds });
  };

  return (
    <div className="space-y-4">
      {/* Distribution IDs Input */}
      <div className="space-y-1">
        <label htmlFor="cloudfront-distribution-ids" className="block text-sm font-medium text-gray-700">
          Distribution IDs
        </label>
        <input
          id="cloudfront-distribution-ids"
          type="text"
          value={distributionIdsInput}
          onChange={(e) => handleDistributionIdsChange(e.target.value)}
          placeholder="E1ABC2DEFGHIJ3, E4XYZ5KLMNOP6"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500">
          Enter CloudFront distribution IDs separated by commas
        </p>
      </div>

      {/* Options */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeCacheRules}
            onChange={(e) => onConfigChange({ includeCacheRules: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Cache Rules</span>
            <p className="text-xs text-gray-500">
              Migrate cache behaviors and TTL settings to Traefik caching middleware
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeOrigins}
            onChange={(e) => onConfigChange({ includeOrigins: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Origins</span>
            <p className="text-xs text-gray-500">
              Migrate origin configurations and routing rules
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeSslCertificates}
            onChange={(e) => onConfigChange({ includeSslCertificates: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include SSL Certificates</span>
            <p className="text-xs text-gray-500">
              Export SSL/TLS certificate configurations
            </p>
          </div>
        </label>
      </div>

      {config.includeSslCertificates && (
        <WarningBox>
          SSL certificates cannot be directly exported from AWS. You will need to manually configure
          certificates using Let&apos;s Encrypt or import your own certificates into Traefik.
        </WarningBox>
      )}
    </div>
  );
}

// ============================================================================
// Route53 Configuration Types and Component
// ============================================================================

interface Route53MigrationSectionProps {
  config: DNSConfig;
  onConfigChange: (config: Partial<DNSConfig>) => void;
}

function Route53MigrationSection({ config, onConfigChange }: Route53MigrationSectionProps) {
  const [hostedZonesInput, setHostedZonesInput] = useState(config.hostedZones.join(', '));

  const handleHostedZonesChange = (value: string) => {
    setHostedZonesInput(value);
    const hostedZones = value
      .split(',')
      .map((zone) => zone.trim())
      .filter((zone) => zone.length > 0);
    onConfigChange({ hostedZones });
  };

  return (
    <div className="space-y-4">
      {/* Hosted Zone IDs Input */}
      <div className="space-y-1">
        <label htmlFor="route53-hosted-zones" className="block text-sm font-medium text-gray-700">
          Hosted Zone IDs
        </label>
        <input
          id="route53-hosted-zones"
          type="text"
          value={hostedZonesInput}
          onChange={(e) => handleHostedZonesChange(e.target.value)}
          placeholder="Z1ABC2DEFG3456, Z7HIJ8KLMN9012"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500">
          Enter Route53 hosted zone IDs separated by commas
        </p>
      </div>

      {/* Export Format Selection */}
      <div className="space-y-2">
        <label className="block text-sm font-medium text-gray-700">
          Export Format
        </label>
        <div className="flex gap-3">
          {[
            { value: 'zone-file', label: 'Zone File' },
            { value: 'json', label: 'JSON' },
          ].map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => onConfigChange({ exportFormat: option.value as DNSConfig['exportFormat'] })}
              className={`px-4 py-2 text-sm rounded-md border transition-colors ${
                config.exportFormat === option.value
                  ? 'bg-primary/10 border-primary text-primary'
                  : 'bg-white border-input text-gray-700 hover:bg-muted'
              }`}
            >
              {option.label}
            </button>
          ))}
        </div>
        <p className="text-xs text-gray-500">
          Zone file format is compatible with BIND and most DNS providers
        </p>
      </div>

      {/* Target DNS Provider */}
      <div className="space-y-1">
        <label htmlFor="target-dns-provider" className="block text-sm font-medium text-gray-700">
          Target DNS Provider <span className="text-muted-foreground/60 font-normal">(optional)</span>
        </label>
        <input
          id="target-dns-provider"
          type="text"
          value={config.targetProvider || ''}
          onChange={(e) => onConfigChange({ targetProvider: e.target.value || undefined })}
          placeholder="e.g., coredns, bind9, cloudflare"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500">
          Specify target provider for optimized export format
        </p>
      </div>

      <InfoBox>
        DNS records will be exported in standard {config.exportFormat === 'zone-file' ? 'BIND zone file' : 'JSON'} format.
        Health check configurations can be migrated to CoreDNS or external monitoring tools.
      </InfoBox>
    </div>
  );
}

// ============================================================================
// Load Balancer Configuration Types and Component
// ============================================================================

interface LoadBalancerConfig {
  enabled: boolean;
  loadBalancerArns: string[];
  includeTargetGroups: boolean;
  includeListeners: boolean;
  includeRoutingRules: boolean;
}

interface LoadBalancerMigrationSectionProps {
  config: LoadBalancerConfig;
  onConfigChange: (config: Partial<LoadBalancerConfig>) => void;
}

function LoadBalancerMigrationSection({ config, onConfigChange }: LoadBalancerMigrationSectionProps) {
  const [lbArnsInput, setLbArnsInput] = useState(config.loadBalancerArns.join(', '));

  const handleLbArnsChange = (value: string) => {
    setLbArnsInput(value);
    const loadBalancerArns = value
      .split(',')
      .map((arn) => arn.trim())
      .filter((arn) => arn.length > 0);
    onConfigChange({ loadBalancerArns });
  };

  return (
    <div className="space-y-4">
      {/* Load Balancer ARNs Input */}
      <div className="space-y-1">
        <label htmlFor="lb-arns" className="block text-sm font-medium text-gray-700">
          Load Balancer ARNs
        </label>
        <input
          id="lb-arns"
          type="text"
          value={lbArnsInput}
          onChange={(e) => handleLbArnsChange(e.target.value)}
          placeholder="arn:aws:elasticloadbalancing:region:account:loadbalancer/..."
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500">
          Enter ALB or NLB ARNs separated by commas
        </p>
      </div>

      {/* Options */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeTargetGroups}
            onChange={(e) => onConfigChange({ includeTargetGroups: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Target Groups</span>
            <p className="text-xs text-gray-500">
              Migrate target group configurations and health checks
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeListeners}
            onChange={(e) => onConfigChange({ includeListeners: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Listeners</span>
            <p className="text-xs text-gray-500">
              Migrate listener configurations and port mappings
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeRoutingRules}
            onChange={(e) => onConfigChange({ includeRoutingRules: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Routing Rules</span>
            <p className="text-xs text-gray-500">
              Migrate path-based and host-based routing rules
            </p>
          </div>
        </label>
      </div>

      <InfoBox>
        Load balancer configurations will be converted to Traefik router and service definitions.
        Target groups will be mapped to Docker service endpoints.
      </InfoBox>
    </div>
  );
}

// ============================================================================
// Main NetworkingMigration Component
// ============================================================================

interface NetworkingMigrationProps {
  resources: Resource[];
}

export function NetworkingMigration({ resources = [] }: NetworkingMigrationProps) {
  const { dns, setDNSConfig } = useMigrationConfigStore();

  // Determine which service cards to show based on discovered resources
  const { hasAPIGateway, hasCloudFront, hasRoute53, hasLB } = useMemo(() => ({
    hasAPIGateway: resources.some(r => API_GATEWAY_TYPES.includes(r.type)),
    hasCloudFront: resources.some(r => CLOUDFRONT_TYPES.includes(r.type)),
    hasRoute53: resources.some(r => ROUTE53_TYPES.includes(r.type)),
    hasLB: resources.some(r => LB_TYPES.includes(r.type)),
  }), [resources]);

  // Local state for API Gateway, CloudFront, Load Balancer (not in current types)
  const [apiGatewayConfig, setAPIGatewayConfig] = useState<APIGatewayConfig>({
    enabled: false,
    apiIds: [],
    targetRouter: 'traefik',
    includeRoutes: true,
    includeAuthConfig: true,
    includeRateLimits: true,
    includeCorsSettings: true,
  });

  const [cloudFrontConfig, setCloudFrontConfig] = useState<CloudFrontConfig>({
    enabled: false,
    distributionIds: [],
    includeCacheRules: true,
    includeOrigins: true,
    includeSslCertificates: false,
  });

  const [loadBalancerConfig, setLoadBalancerConfig] = useState<LoadBalancerConfig>({
    enabled: false,
    loadBalancerArns: [],
    includeTargetGroups: true,
    includeListeners: true,
    includeRoutingRules: true,
  });

  const handleAPIGatewayChange = (config: Partial<APIGatewayConfig>) => {
    setAPIGatewayConfig((prev) => ({ ...prev, ...config }));
  };

  const handleCloudFrontChange = (config: Partial<CloudFrontConfig>) => {
    setCloudFrontConfig((prev) => ({ ...prev, ...config }));
  };

  const handleLoadBalancerChange = (config: Partial<LoadBalancerConfig>) => {
    setLoadBalancerConfig((prev) => ({ ...prev, ...config }));
  };

  // If no networking resources discovered, show empty state
  if (!hasAPIGateway && !hasCloudFront && !hasRoute53 && !hasLB) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        <Globe className="w-12 h-12 mx-auto mb-3 opacity-50" />
        <p className="font-medium">No networking resources discovered</p>
        <p className="text-sm mt-1">API Gateway, CloudFront, Route53, or Load Balancer resources will appear here when detected.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* API Gateway Section - only show if API Gateway resources discovered */}
      {hasAPIGateway && (
        <ServiceMigrationCard
          title="API Gateway -> Traefik/Kong"
          description="Migrate API routes and configurations"
          icon={Globe}
          enabled={apiGatewayConfig.enabled}
          onToggle={(enabled) => handleAPIGatewayChange({ enabled })}
          defaultExpanded={true}
        >
          <APIGatewayMigrationSection
            config={apiGatewayConfig}
            onConfigChange={handleAPIGatewayChange}
          />
        </ServiceMigrationCard>
      )}

      {/* CloudFront Section - only show if CloudFront resources discovered */}
      {hasCloudFront && (
        <ServiceMigrationCard
          title="CloudFront -> Traefik"
          description="Migrate CDN configurations"
          icon={Cloud}
          enabled={cloudFrontConfig.enabled}
          onToggle={(enabled) => handleCloudFrontChange({ enabled })}
          defaultExpanded={true}
        >
          <CloudFrontMigrationSection
            config={cloudFrontConfig}
            onConfigChange={handleCloudFrontChange}
          />
        </ServiceMigrationCard>
      )}

      {/* Route53 Section - only show if Route53 resources discovered */}
      {hasRoute53 && (
        <ServiceMigrationCard
          title="Route53 -> CoreDNS"
          description="Export DNS zones and records"
          icon={Globe}
          enabled={dns.enabled}
          onToggle={(enabled) => setDNSConfig({ enabled })}
          defaultExpanded={true}
        >
          <Route53MigrationSection
            config={dns}
            onConfigChange={setDNSConfig}
          />
        </ServiceMigrationCard>
      )}

      {/* Load Balancer Section - only show if LB resources discovered */}
      {hasLB && (
        <ServiceMigrationCard
          title="ALB/NLB -> Traefik"
          description="Convert load balancer configurations"
          icon={Network}
          enabled={loadBalancerConfig.enabled}
          onToggle={(enabled) => handleLoadBalancerChange({ enabled })}
          defaultExpanded={true}
        >
          <LoadBalancerMigrationSection
            config={loadBalancerConfig}
            onConfigChange={handleLoadBalancerChange}
          />
        </ServiceMigrationCard>
      )}
    </div>
  );
}

export default NetworkingMigration;
