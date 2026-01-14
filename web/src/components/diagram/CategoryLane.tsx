import { memo } from 'react';
import { type NodeProps } from '@xyflow/react';
import {
  Server,
  Database,
  HardDrive,
  Network,
  Shield,
  MessageSquare,
  Cloud,
  Zap,
  type LucideIcon,
} from 'lucide-react';

interface CategoryLaneData {
  category: string;
  label: string;
  color: string;
  width: number;
  height: number;
  [key: string]: unknown;
}

const categoryIcons: Record<string, LucideIcon> = {
  compute: Server,
  serverless: Zap,
  storage: HardDrive,
  object_storage: Cloud,
  database: Database,
  sql_database: Database,
  networking: Network,
  security: Shield,
  messaging: MessageSquare,
  queue: MessageSquare,
};

function getCategoryIcon(category: string): LucideIcon {
  const lowerCat = category.toLowerCase();

  if (lowerCat.includes('serverless') || lowerCat.includes('lambda') || lowerCat.includes('function')) return Zap;
  if (lowerCat.includes('compute')) return Server;
  if (lowerCat.includes('object') || lowerCat.includes('s3') || lowerCat.includes('bucket')) return Cloud;
  if (lowerCat.includes('storage')) return HardDrive;
  if (lowerCat.includes('database') || lowerCat.includes('sql') || lowerCat.includes('rds')) return Database;
  if (lowerCat.includes('network')) return Network;
  if (lowerCat.includes('security') || lowerCat.includes('iam')) return Shield;
  if (lowerCat.includes('messag') || lowerCat.includes('queue') || lowerCat.includes('sqs') || lowerCat.includes('sns')) return MessageSquare;

  return categoryIcons[category] || Server;
}

function CategoryLaneComponent({ data }: NodeProps) {
  const { label, color, width, height, category } = data as CategoryLaneData;
  const Icon = getCategoryIcon(category);

  return (
    <div
      className="relative rounded-2xl pointer-events-none"
      style={{
        width: Math.max(width, 260),
        height: Math.max(height, 150),
        background: `linear-gradient(180deg, ${color}10 0%, ${color}05 100%)`,
        border: `2px solid ${color}25`,
        boxShadow: `0 4px 24px ${color}12`,
      }}
    >
      {/* Header - Floating badge style above the lane */}
      <div
        className="absolute -top-4 left-4 flex items-center gap-2 px-4 py-2 rounded-full shadow-lg pointer-events-none"
        style={{
          background: `linear-gradient(135deg, ${color} 0%, ${color}cc 100%)`,
          boxShadow: `0 4px 16px ${color}50`,
        }}
      >
        <div
          className="w-6 h-6 rounded-full flex items-center justify-center"
          style={{ backgroundColor: 'rgba(255,255,255,0.2)' }}
        >
          <Icon className="w-4 h-4 text-white" />
        </div>
        <span className="font-semibold text-xs uppercase tracking-wide text-white drop-shadow-sm">
          {label}
        </span>
      </div>
      {/* Subtle corner accent */}
      <div
        className="absolute top-0 right-0 w-20 h-20 rounded-bl-full opacity-30"
        style={{ background: `linear-gradient(135deg, ${color}20, transparent)` }}
      />
    </div>
  );
}

export const CategoryLane = memo(CategoryLaneComponent);
