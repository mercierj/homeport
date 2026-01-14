import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Check, AlertTriangle, Server, Database, HardDrive, Network, Shield, MessageSquare, type LucideIcon } from 'lucide-react';
import type { ResourceNode as ResourceNodeType } from '../../lib/diagram-types';
import { categoryColors } from '../../lib/diagram-types';

const categoryIcons: Record<string, LucideIcon> = {
  compute: Server,
  storage: HardDrive,
  database: Database,
  networking: Network,
  security: Shield,
  messaging: MessageSquare,
};

function ResourceNodeComponent({ data }: NodeProps<ResourceNodeType>) {
  const { resource, selected, hasWarning } = data;
  const bgColor = categoryColors[resource.category] || '#6b7280';
  const Icon = categoryIcons[resource.category] || Server;

  return (
    <div
      className={`
        relative px-4 py-3 rounded-xl shadow-lg min-w-[180px] max-w-[200px] cursor-pointer
        transition-all duration-200 hover:shadow-xl hover:scale-[1.02]
        ${selected
          ? 'bg-white ring-2 ring-emerald-500 ring-offset-2'
          : 'bg-white/80 border-2 border-dashed border-input opacity-60 hover:opacity-80'
        }
        ${hasWarning ? 'ring-amber-500' : ''}
      `}
    >
      {/* Header with icon and category */}
      <div className="flex items-center gap-2 mb-2">
        <div
          className="w-7 h-7 rounded-lg flex items-center justify-center"
          style={{ backgroundColor: `${bgColor}20` }}
        >
          <Icon className="w-4 h-4" style={{ color: bgColor }} />
        </div>
        <span
          className="text-xs font-semibold uppercase tracking-wide"
          style={{ color: bgColor }}
        >
          {resource.category}
        </span>
      </div>

      {/* Resource info */}
      <div className="font-semibold text-gray-900 text-sm leading-tight truncate">
        {resource.name}
      </div>
      <div className="text-xs text-gray-500 mt-0.5 truncate">
        {resource.type}
      </div>

      {/* Selection/warning indicators */}
      <div className="absolute top-2 right-2 flex gap-1">
        {hasWarning && (
          <div className="w-5 h-5 bg-warning/20 rounded-full flex items-center justify-center">
            <AlertTriangle className="w-3.5 h-3.5 text-warning" />
          </div>
        )}
        {selected && !hasWarning && (
          <div className="w-5 h-5 bg-success rounded-full flex items-center justify-center shadow-sm">
            <Check className="w-3 h-3 text-white" />
          </div>
        )}
      </div>

      {/* React Flow handles for edges */}
      <Handle
        type="target"
        position={Position.Top}
        className="!w-3 !h-3 !bg-slate-400 !border-2 !border-white"
      />
      <Handle
        type="source"
        position={Position.Bottom}
        className="!w-3 !h-3 !bg-slate-400 !border-2 !border-white"
      />
    </div>
  );
}

export const ResourceNode = memo(ResourceNodeComponent);
