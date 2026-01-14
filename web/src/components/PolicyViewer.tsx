import { useState } from 'react';
import { ChevronDown, ChevronUp, AlertTriangle, Copy, Check, FileJson, List } from 'lucide-react';
import type { Policy } from '@/lib/policy-types';
import {
  formatPolicyType,
  formatProvider,
  policyTypeBadgeClasses,
  providerBadgeClasses,
  getConfidenceColor,
  getConfidenceLabel,
} from '@/lib/policy-types';

interface PolicyViewerProps {
  policy: Policy;
  onEdit?: () => void;
  onDelete?: () => void;
  defaultExpanded?: boolean;
}

export function PolicyViewer({ policy, onEdit, onDelete, defaultExpanded = false }: PolicyViewerProps) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [viewMode, setViewMode] = useState<'normalized' | 'raw'>('normalized');
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    const content =
      viewMode === 'raw'
        ? JSON.stringify(policy.original_document, null, 2)
        : JSON.stringify(policy.normalized_policy, null, 2);

    await navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="card-resource">
      {/* Header */}
      <div
        className="flex items-center justify-between cursor-pointer"
        onClick={() => setExpanded(!expanded)}
      >
        <div className="flex items-center gap-3">
          <div className="flex flex-col">
            <div className="flex items-center gap-2">
              <h4 className="font-medium">{policy.name}</h4>
              {policy.warnings && policy.warnings.length > 0 && (
                <span className="flex items-center gap-1 text-yellow-600" title={`${policy.warnings.length} warning(s)`}>
                  <AlertTriangle className="h-4 w-4" />
                  <span className="text-xs">{policy.warnings.length}</span>
                </span>
              )}
            </div>
            <p className="text-sm text-muted-foreground">{policy.resource_name}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span className={policyTypeBadgeClasses[policy.type]}>{formatPolicyType(policy.type)}</span>
          <span className={providerBadgeClasses[policy.provider]}>{formatProvider(policy.provider)}</span>
          {policy.keycloak_mapping && (
            <span
              className={`text-xs ${getConfidenceColor(policy.keycloak_mapping.mapping_confidence)}`}
              title={`Mapping confidence: ${Math.round(policy.keycloak_mapping.mapping_confidence * 100)}%`}
            >
              {getConfidenceLabel(policy.keycloak_mapping.mapping_confidence)}
            </span>
          )}
          {expanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
        </div>
      </div>

      {/* Expanded Content */}
      {expanded && (
        <div className="mt-4 border-t border-border pt-4 space-y-4">
          {/* Warnings */}
          {policy.warnings && policy.warnings.length > 0 && (
            <div className="alert-warning">
              <AlertTriangle className="h-4 w-4 flex-shrink-0" />
              <div className="space-y-1">
                {policy.warnings.map((warning, i) => (
                  <p key={i} className="text-sm">
                    {warning}
                  </p>
                ))}
              </div>
            </div>
          )}

          {/* View Mode Toggle */}
          <div className="flex items-center justify-between">
            <div className="flex gap-2">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setViewMode('normalized');
                }}
                className={`flex items-center gap-1 px-3 py-1 rounded text-sm ${
                  viewMode === 'normalized'
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-muted hover:bg-muted/80'
                }`}
              >
                <List className="h-3 w-3" />
                Normalized
              </button>
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setViewMode('raw');
                }}
                className={`flex items-center gap-1 px-3 py-1 rounded text-sm ${
                  viewMode === 'raw'
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-muted hover:bg-muted/80'
                }`}
              >
                <FileJson className="h-3 w-3" />
                Raw JSON
              </button>
            </div>
            <button
              onClick={(e) => {
                e.stopPropagation();
                handleCopy();
              }}
              className="flex items-center gap-1 px-2 py-1 text-sm text-muted-foreground hover:text-foreground"
            >
              {copied ? <Check className="h-4 w-4 text-green-500" /> : <Copy className="h-4 w-4" />}
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>

          {/* Policy Content */}
          <div className="code-block max-h-96 overflow-auto">
            <pre className="text-xs">
              {viewMode === 'normalized'
                ? JSON.stringify(policy.normalized_policy, null, 2)
                : JSON.stringify(policy.original_document, null, 2)}
            </pre>
          </div>

          {/* Keycloak Mapping Preview */}
          {policy.keycloak_mapping && policy.type !== 'network' && (
            <div className="border border-border rounded-lg p-4">
              <h5 className="font-medium mb-3 flex items-center gap-2">
                Keycloak Mapping
                <span
                  className={`text-xs ${getConfidenceColor(policy.keycloak_mapping.mapping_confidence)}`}
                >
                  ({Math.round(policy.keycloak_mapping.mapping_confidence * 100)}% confidence)
                </span>
              </h5>

              {/* Roles */}
              {policy.keycloak_mapping.roles.length > 0 && (
                <div className="mb-3">
                  <p className="text-sm text-muted-foreground mb-1">Generated Roles:</p>
                  <div className="flex flex-wrap gap-2">
                    {policy.keycloak_mapping.roles.map((role, i) => (
                      <span key={i} className="badge-outline text-xs">
                        {role.name}
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {/* Unmapped Actions */}
              {policy.keycloak_mapping.unmapped_actions && policy.keycloak_mapping.unmapped_actions.length > 0 && (
                <div className="mb-3">
                  <p className="text-sm text-yellow-600 mb-1">Unmapped Actions:</p>
                  <div className="flex flex-wrap gap-2">
                    {policy.keycloak_mapping.unmapped_actions.map((action, i) => (
                      <span key={i} className="text-xs bg-yellow-100 text-yellow-800 px-2 py-0.5 rounded">
                        {action}
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {/* Review Notes */}
              {policy.keycloak_mapping.manual_review_notes && policy.keycloak_mapping.manual_review_notes.length > 0 && (
                <div>
                  <p className="text-sm text-muted-foreground mb-1">Review Notes:</p>
                  <ul className="list-disc list-inside text-sm text-muted-foreground">
                    {policy.keycloak_mapping.manual_review_notes.map((note, i) => (
                      <li key={i}>{note}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-2 border-t border-border">
            {onEdit && policy.type !== 'network' && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onEdit();
                }}
                className="px-3 py-1 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90"
              >
                Edit Policy
              </button>
            )}
            {onDelete && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onDelete();
                }}
                className="px-3 py-1 text-sm bg-error text-error-foreground rounded hover:bg-error/90"
              >
                Delete
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
