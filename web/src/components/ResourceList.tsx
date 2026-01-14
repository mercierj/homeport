import { cn } from '@/lib/utils';
import { categoryBadgeClasses, categoryIconClasses } from '@/lib/diagram-types';
import { Server, Database, HardDrive, Globe, Shield, MessageSquare } from 'lucide-react';

interface Resource {
  id: string;
  name: string;
  type: string;
  category: string;
  dependencies: string[];
}

interface ResourceListProps {
  resources: Resource[];
  className?: string;
}

const categoryIcons: Record<string, React.ReactNode> = {
  compute: <Server className="h-5 w-5" />,
  database: <Database className="h-5 w-5" />,
  storage: <HardDrive className="h-5 w-5" />,
  networking: <Globe className="h-5 w-5" />,
  security: <Shield className="h-5 w-5" />,
  messaging: <MessageSquare className="h-5 w-5" />,
};

export function ResourceList({ resources, className }: ResourceListProps) {
  if (resources.length === 0) {
    return (
      <div className={cn("empty-state", className)}>
        <p className="empty-state-description">No resources detected</p>
      </div>
    );
  }

  return (
    <div className={cn("space-y-2", className)}>
      {resources.map((resource) => (
        <div
          key={resource.id}
          className="card-resource"
        >
          <div className="flex items-center gap-4">
            <div className={cn(categoryIconClasses[resource.category] || 'resource-icon-compute')}>
              {categoryIcons[resource.category] || <Server className="h-5 w-5" />}
            </div>
            <div className="flex-1">
              <p className="font-medium">{resource.name}</p>
              <p className="text-sm text-muted-foreground">{resource.type}</p>
            </div>
            <span className={cn(categoryBadgeClasses[resource.category] || 'badge-secondary')}>
              {resource.category}
            </span>
          </div>
          {resource.dependencies.length > 0 && (
            <p className="text-xs text-muted-foreground mt-2">
              {resource.dependencies.length} dependencies
            </p>
          )}
        </div>
      ))}
    </div>
  );
}
