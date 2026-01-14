import React, { useState } from 'react';
import { ChevronDown, ChevronUp } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

interface ServiceMigrationCardProps {
  title: string;
  description: string;
  icon: LucideIcon;
  enabled: boolean;
  onToggle: (enabled: boolean) => void;
  children: React.ReactNode;
  defaultExpanded?: boolean;
}

export function ServiceMigrationCard({
  title,
  description,
  icon: Icon,
  enabled,
  onToggle,
  children,
  defaultExpanded = false,
}: ServiceMigrationCardProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  const handleToggle = () => {
    onToggle(!enabled);
  };

  const handleExpandToggle = () => {
    if (enabled) {
      setIsExpanded(!isExpanded);
    }
  };

  const showContent = enabled && isExpanded;

  return (
    <div
      className={`rounded-lg border shadow-sm transition-colors duration-200 ${
        enabled
          ? 'border-success/30 bg-success/5'
          : 'border-border bg-background'
      }`}
    >
      {/* Header */}
      <div className="flex items-center justify-between p-4">
        {/* Left: Toggle Switch */}
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          onClick={handleToggle}
          className={`toggle cursor-pointer focus:outline-none focus:ring-2 focus:ring-success focus:ring-offset-2 ${
            enabled ? 'toggle-enabled' : 'toggle-disabled'
          }`}
        >
          <span
            className={`toggle-thumb ${
              enabled ? 'translate-x-5' : 'translate-x-0'
            }`}
          />
        </button>

        {/* Center: Icon, Title, Description */}
        <div className="flex flex-1 items-center gap-3 px-4">
          <div
            className={`flex h-10 w-10 items-center justify-center rounded-lg ${
              enabled ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'
            }`}
          >
            <Icon className="h-5 w-5" />
          </div>
          <div className="flex flex-col">
            <span
              className={`font-medium ${
                enabled ? 'text-foreground' : 'text-muted-foreground'
              }`}
            >
              {title}
            </span>
            <span className="text-sm text-muted-foreground">{description}</span>
          </div>
        </div>

        {/* Right: Expand/Collapse Chevron */}
        <button
          type="button"
          onClick={handleExpandToggle}
          disabled={!enabled}
          className={`flex h-8 w-8 items-center justify-center rounded-md transition-colors ${
            enabled
              ? 'text-muted-foreground hover:bg-muted hover:text-foreground'
              : 'cursor-not-allowed text-muted-foreground/40'
          }`}
          aria-label={isExpanded ? 'Collapse options' : 'Expand options'}
        >
          {isExpanded ? (
            <ChevronUp className="h-5 w-5" />
          ) : (
            <ChevronDown className="h-5 w-5" />
          )}
        </button>
      </div>

      {/* Collapsible Content */}
      <div
        className={`overflow-hidden transition-all duration-300 ease-in-out ${
          showContent ? 'max-h-[2000px] opacity-100' : 'max-h-0 opacity-0'
        }`}
      >
        <div className="border-t border-gray-200 p-4">{children}</div>
      </div>
    </div>
  );
}

export default ServiceMigrationCard;
