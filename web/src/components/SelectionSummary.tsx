import { useMemo } from 'react';
import { Check, AlertTriangle, CheckCircle2, XCircle } from 'lucide-react';
import type { Resource } from '../lib/migrate-api';
import { categoryColors } from '../lib/diagram-types';

interface SelectionSummaryProps {
  resources: Resource[];
  selected: Set<string>;
  onSelectAll: () => void;
  onSelectNone: () => void;
}

interface DependencyWarning {
  resourceId: string;
  resourceName: string;
  missingDependency: string;
  missingDependencyName: string;
}

export function SelectionSummary({ resources, selected, onSelectAll, onSelectNone }: SelectionSummaryProps) {
  // Calculate category breakdown
  const categoryStats = useMemo(() => {
    const stats = new Map<string, { total: number; selected: number }>();

    for (const res of resources) {
      const cat = res.category || 'other';
      if (!stats.has(cat)) {
        stats.set(cat, { total: 0, selected: 0 });
      }
      const s = stats.get(cat)!;
      s.total++;
      if (selected.has(res.id)) {
        s.selected++;
      }
    }

    return stats;
  }, [resources, selected]);

  // Calculate region breakdown
  const regionStats = useMemo(() => {
    const stats = new Map<string, { total: number; selected: number }>();

    for (const res of resources) {
      const region = res.region || 'unknown';
      if (!stats.has(region)) {
        stats.set(region, { total: 0, selected: 0 });
      }
      const s = stats.get(region)!;
      s.total++;
      if (selected.has(res.id)) {
        s.selected++;
      }
    }

    return stats;
  }, [resources, selected]);

  // Calculate dependency warnings
  const warnings = useMemo(() => {
    const result: DependencyWarning[] = [];
    const resourceMap = new Map(resources.map(r => [r.id, r]));

    for (const res of resources) {
      if (!selected.has(res.id)) continue;

      for (const depId of res.dependencies || []) {
        if (!selected.has(depId)) {
          const depResource = resourceMap.get(depId);
          result.push({
            resourceId: res.id,
            resourceName: res.name,
            missingDependency: depId,
            missingDependencyName: depResource?.name || depId,
          });
        }
      }
    }

    return result;
  }, [resources, selected]);

  const selectedCount = selected.size;
  const totalCount = resources.length;
  const allSelected = selectedCount === totalCount;
  const noneSelected = selectedCount === 0;

  return (
    <div className="bg-white rounded-lg border p-4 space-y-4">
      {/* Header with count */}
      <div className="flex items-center justify-between">
        <div>
          <div className="text-lg font-semibold text-foreground">
            {selectedCount} of {totalCount} resources
          </div>
          <div className="text-sm text-muted-foreground">selected for migration</div>
        </div>

        {/* Quick actions */}
        <div className="flex gap-2">
          <button
            onClick={onSelectAll}
            disabled={allSelected}
            className="px-3 py-1.5 text-sm font-medium rounded-md bg-accent/10 text-accent hover:bg-accent/20 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-1"
          >
            <CheckCircle2 className="w-4 h-4" />
            All
          </button>
          <button
            onClick={onSelectNone}
            disabled={noneSelected}
            className="px-3 py-1.5 text-sm font-medium rounded-md bg-muted text-muted-foreground hover:bg-muted/80 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-1"
          >
            <XCircle className="w-4 h-4" />
            None
          </button>
        </div>
      </div>

      {/* Category breakdown */}
      <div className="space-y-2">
        <div className="text-sm font-medium text-foreground">By Category</div>
        <div className="grid grid-cols-2 gap-2">
          {Array.from(categoryStats.entries()).map(([category, stats]) => (
            <div
              key={category}
              className="flex items-center justify-between px-3 py-2 bg-muted rounded-md"
            >
              <div className="flex items-center gap-2">
                <div
                  className="w-3 h-3 rounded-full"
                  style={{ backgroundColor: categoryColors[category] || '#6b7280' }}
                />
                <span className="text-sm text-muted-foreground capitalize">{category}</span>
              </div>
              <span className="text-sm font-medium text-foreground">
                {stats.selected}/{stats.total}
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* Region breakdown */}
      {regionStats.size > 0 && !regionStats.has('unknown') && (
        <div className="space-y-2">
          <div className="text-sm font-medium text-foreground">By Region</div>
          <div className="max-h-40 overflow-y-auto space-y-1.5">
            {Array.from(regionStats.entries())
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([region, stats]) => (
              <div
                key={region}
                className="flex items-center justify-between px-3 py-1.5 bg-info/10 rounded-md"
              >
                <span className="text-sm text-info font-mono">{region}</span>
                <span className="text-sm font-medium text-info">
                  {stats.selected}/{stats.total}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Dependency warnings */}
      {warnings.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-sm font-medium text-amber-700">
            <AlertTriangle className="w-4 h-4" />
            Dependency Warnings ({warnings.length})
          </div>
          <div className="max-h-32 overflow-y-auto space-y-1">
            {warnings.map((w, i) => (
              <div
                key={i}
                className="text-xs text-muted-foreground bg-warning/10 px-3 py-2 rounded"
              >
                <span className="font-medium">{w.resourceName}</span>
                {' depends on '}
                <span className="font-medium text-warning">{w.missingDependencyName}</span>
                {' (not selected)'}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* All good indicator */}
      {warnings.length === 0 && selectedCount > 0 && (
        <div className="flex items-center gap-2 text-sm text-accent bg-accent/10 px-3 py-2 rounded">
          <Check className="w-4 h-4" />
          All dependencies satisfied
        </div>
      )}
    </div>
  );
}
