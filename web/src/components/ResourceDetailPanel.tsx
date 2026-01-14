import { X, Server, Database, HardDrive, Network, Shield, MessageSquare, Layers, Clock, Cpu, MemoryStick, Globe, Tag, Box, Activity, type LucideIcon } from 'lucide-react';
import type { Resource } from '../lib/migrate-api';
import { categoryColors } from '../lib/diagram-types';

interface ResourceDetailPanelProps {
  resource: Resource;
  onClose: () => void;
}

const categoryIcons: Record<string, LucideIcon> = {
  compute: Server,
  serverless: Layers,
  storage: HardDrive,
  object_storage: HardDrive,
  database: Database,
  networking: Network,
  security: Shield,
  messaging: MessageSquare,
};

// Parse resource type to get service info
function getServiceInfo(type: string): { provider: string; service: string } {
  // AWS types: aws_lambda_function, AWS::Lambda::Function
  if (type.startsWith('aws_') || type.startsWith('AWS::')) {
    const parts = type.replace('AWS::', '').replace('aws_', '').split(/[_:]/);
    return { provider: 'AWS', service: parts.join(' ') };
  }
  // GCP types: google_cloudfunctions_function
  if (type.startsWith('google_')) {
    const parts = type.replace('google_', '').split('_');
    return { provider: 'GCP', service: parts.join(' ') };
  }
  // Azure types: azurerm_function_app, Microsoft.Web/sites
  if (type.startsWith('azurerm_') || type.startsWith('Microsoft.')) {
    const parts = type.replace('azurerm_', '').replace('Microsoft.', '').split(/[_./]/);
    return { provider: 'Azure', service: parts.join(' ') };
  }
  // Docker/self-hosted
  return { provider: 'Self-Hosted', service: type };
}

// Get relevant detail fields based on resource type
function getDetailFields(resource: Resource): Array<{ icon: LucideIcon; label: string; value: string }> {
  const fields: Array<{ icon: LucideIcon; label: string; value: string }> = [];
  const tags = resource.tags || {};
  const type = resource.type.toLowerCase();

  // Lambda/Functions
  if (type.includes('lambda') || type.includes('function') || type.includes('cloudfunctions')) {
    if (tags.runtime) fields.push({ icon: Cpu, label: 'Runtime', value: tags.runtime });
    if (tags.memory) fields.push({ icon: MemoryStick, label: 'Memory', value: `${tags.memory} MB` });
    if (tags.timeout) fields.push({ icon: Clock, label: 'Timeout', value: `${tags.timeout}s` });
    if (tags.handler) fields.push({ icon: Layers, label: 'Handler', value: tags.handler });
    if (tags.version) fields.push({ icon: Tag, label: 'Version', value: tags.version });
    if (tags.layers) fields.push({ icon: Layers, label: 'Layers', value: tags.layers });
    if (tags.codeSize) fields.push({ icon: HardDrive, label: 'Code Size', value: formatBytes(parseInt(tags.codeSize)) });
  }

  // EC2/Compute
  if (type.includes('ec2') || type.includes('instance') || type.includes('compute')) {
    if (tags.instanceType) fields.push({ icon: Server, label: 'Instance Type', value: tags.instanceType });
    if (tags.ami) fields.push({ icon: Box, label: 'AMI', value: tags.ami });
    if (tags.state) fields.push({ icon: Activity, label: 'State', value: tags.state });
    if (tags.publicIp) fields.push({ icon: Globe, label: 'Public IP', value: tags.publicIp });
    if (tags.privateIp) fields.push({ icon: Network, label: 'Private IP', value: tags.privateIp });
  }

  // RDS/Database
  if (type.includes('rds') || type.includes('db_instance') || type.includes('sql')) {
    if (tags.engine) fields.push({ icon: Database, label: 'Engine', value: tags.engine });
    if (tags.engineVersion) fields.push({ icon: Tag, label: 'Version', value: tags.engineVersion });
    if (tags.instanceClass) fields.push({ icon: Server, label: 'Instance Class', value: tags.instanceClass });
    if (tags.storage) fields.push({ icon: HardDrive, label: 'Storage', value: `${tags.storage} GB` });
    if (tags.multiAz) fields.push({ icon: Globe, label: 'Multi-AZ', value: tags.multiAz });
  }

  // S3/Storage
  if (type.includes('s3') || type.includes('bucket') || type.includes('storage')) {
    if (tags.region) fields.push({ icon: Globe, label: 'Region', value: tags.region });
    if (tags.versioning) fields.push({ icon: Tag, label: 'Versioning', value: tags.versioning });
    if (tags.encryption) fields.push({ icon: Shield, label: 'Encryption', value: tags.encryption });
  }

  // Container/Docker
  if (tags.image) fields.push({ icon: Box, label: 'Image', value: tags.image });
  if (tags.ports) fields.push({ icon: Network, label: 'Ports', value: tags.ports });
  if (tags.status) fields.push({ icon: Activity, label: 'Status', value: tags.status });
  if (tags.created) fields.push({ icon: Clock, label: 'Created', value: formatDate(tags.created) });

  // Generic tags fallback
  if (fields.length === 0) {
    Object.entries(tags).slice(0, 6).forEach(([key, value]) => {
      if (value && typeof value === 'string' && !key.startsWith('aws:') && !key.startsWith('com.')) {
        fields.push({ icon: Tag, label: formatLabel(key), value: value });
      }
    });
  }

  return fields;
}

function formatBytes(bytes: number): string {
  if (isNaN(bytes)) return 'N/A';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDate(dateStr: string): string {
  try {
    return new Date(dateStr).toLocaleDateString();
  } catch {
    return dateStr;
  }
}

function formatLabel(key: string): string {
  return key
    .replace(/([A-Z])/g, ' $1')
    .replace(/[_-]/g, ' ')
    .replace(/^\w/, c => c.toUpperCase())
    .trim();
}

export function ResourceDetailPanel({ resource, onClose }: ResourceDetailPanelProps) {
  const bgColor = categoryColors[resource.category] || '#6b7280';
  const Icon = categoryIcons[resource.category] || Server;
  const { provider, service } = getServiceInfo(resource.type);
  const fields = getDetailFields(resource);

  return (
    <div className="bg-white border-b shadow-sm animate-in slide-in-from-top-2 duration-200">
      <div className="max-w-6xl mx-auto px-6 py-4">
        <div className="flex items-start justify-between gap-4">
          {/* Left: Resource info */}
          <div className="flex items-start gap-4 min-w-0 flex-1">
            {/* Icon */}
            <div
              className="w-12 h-12 rounded-xl flex items-center justify-center flex-shrink-0"
              style={{ backgroundColor: `${bgColor}15` }}
            >
              <Icon className="w-6 h-6" style={{ color: bgColor }} />
            </div>

            {/* Name and type */}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 mb-1">
                <h3 className="font-semibold text-lg truncate">{resource.name}</h3>
                <span
                  className="px-2 py-0.5 rounded text-xs font-medium uppercase flex-shrink-0"
                  style={{ backgroundColor: `${bgColor}15`, color: bgColor }}
                >
                  {resource.category}
                </span>
              </div>
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <span className="px-1.5 py-0.5 bg-muted rounded text-xs font-medium">{provider}</span>
                {resource.region && (
                  <span className="px-1.5 py-0.5 bg-info/10 text-info rounded text-xs font-medium">
                    {resource.region}
                  </span>
                )}
                <span className="capitalize">{service}</span>
              </div>
            </div>
          </div>

          {/* Close button */}
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-muted rounded-lg transition-colors flex-shrink-0"
          >
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>

        {/* Detail fields */}
        {fields.length > 0 && (
          <div className="mt-4 grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
            {fields.map((field, i) => (
              <div key={i} className="p-2.5 bg-muted rounded-lg">
                <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1">
                  <field.icon className="w-3.5 h-3.5" />
                  {field.label}
                </div>
                <div className="font-medium text-sm truncate" title={field.value}>
                  {field.value}
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Dependencies */}
        {resource.dependencies && resource.dependencies.length > 0 && (
          <div className="mt-3 flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">Depends on:</span>
            <div className="flex flex-wrap gap-1.5">
              {resource.dependencies.map((dep, i) => (
                <span key={i} className="px-2 py-0.5 bg-muted rounded text-xs font-medium">
                  {dep}
                </span>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
