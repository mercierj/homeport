import {
  Database,
  HardDrive,
  MessageSquare,
  Zap,
  Shield,
  Key,
  Globe,
  Code,
  Check,
  type LucideIcon,
} from 'lucide-react';
import type { MigrationCategory } from './types';
import { MIGRATION_CATEGORIES, CATEGORY_LABELS } from './types';

// ============================================================================
// Types
// ============================================================================

interface ServiceCategoryTabsProps {
  activeCategory: MigrationCategory;
  onCategoryChange: (category: MigrationCategory) => void;
  enabledCategories: MigrationCategory[];
}

// ============================================================================
// Category Icons Mapping
// ============================================================================

const CATEGORY_ICONS: Record<MigrationCategory, LucideIcon> = {
  database: Database,
  storage: HardDrive,
  queue: MessageSquare,
  cache: Zap,
  auth: Shield,
  secrets: Key,
  dns: Globe,
  functions: Code,
};

// ============================================================================
// Badge Component
// ============================================================================

interface StatusBadgeProps {
  enabled: boolean;
}

function StatusBadge({ enabled }: StatusBadgeProps) {
  if (enabled) {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-success/10 text-success">
        <Check className="w-3 h-3" />
        <span>On</span>
      </span>
    );
  }

  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-muted text-muted-foreground">
      Off
    </span>
  );
}

// ============================================================================
// Tab Item Component
// ============================================================================

interface TabItemProps {
  category: MigrationCategory;
  isActive: boolean;
  isEnabled: boolean;
  onClick: () => void;
}

function TabItem({ category, isActive, isEnabled, onClick }: TabItemProps) {
  const Icon = CATEGORY_ICONS[category];
  const label = CATEGORY_LABELS[category];

  return (
    <button
      type="button"
      role="tab"
      aria-selected={isActive}
      onClick={onClick}
      className={`
        w-full flex items-center gap-3 px-4 py-3 text-left
        transition-all duration-150 ease-in-out
        focus:outline-none focus:ring-2 focus:ring-primary focus:ring-inset
        ${
          isActive
            ? 'tab-active'
            : 'tab-inactive hover:text-foreground'
        }
      `}
    >
      <Icon
        className={`w-5 h-5 flex-shrink-0 ${
          isActive ? 'text-primary' : 'text-muted-foreground/60'
        }`}
      />
      <span className="flex-1 font-medium text-sm">{label}</span>
      <StatusBadge enabled={isEnabled} />
    </button>
  );
}

// ============================================================================
// Main Component
// ============================================================================

export function ServiceCategoryTabs({
  activeCategory,
  onCategoryChange,
  enabledCategories,
}: ServiceCategoryTabsProps) {
  return (
    <nav className="w-64 flex-shrink-0" role="tablist" aria-label="Migration categories">
      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        {MIGRATION_CATEGORIES.map((category) => (
          <TabItem
            key={category}
            category={category}
            isActive={activeCategory === category}
            isEnabled={enabledCategories.includes(category)}
            onClick={() => onCategoryChange(category)}
          />
        ))}
      </div>
    </nav>
  );
}

export default ServiceCategoryTabs;
