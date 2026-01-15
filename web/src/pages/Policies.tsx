import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Shield, Key, AlertTriangle, RefreshCw, Download, Trash2 } from 'lucide-react';
import { PolicyList } from '@/components/PolicyList';
import { PolicyEditor } from '@/components/PolicyEditor';
import { RBACMapper } from '@/components/RBACMapper';
import {
  listPolicies,
  deletePolicy,
  getPolicySummary,
} from '@/lib/policy-api';
import type { Policy, PolicySummary } from '@/lib/policy-types';

type TabType = 'list' | 'rbac';

export default function Policies() {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<TabType>('list');
  const [editingPolicy, setEditingPolicy] = useState<Policy | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<Policy | null>(null);

  // Fetch policies
  const {
    data: policyData,
    isLoading: policiesLoading,
    error: policiesError,
    refetch: refetchPolicies,
  } = useQuery({
    queryKey: ['policies'],
    queryFn: () => listPolicies(),
  });

  // Fetch summary
  const { data: summary } = useQuery<PolicySummary>({
    queryKey: ['policies', 'summary'],
    queryFn: getPolicySummary,
  });

  // Delete mutation
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deletePolicy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setDeleteConfirm(null);
    },
  });

  const handleEdit = (policy: Policy) => {
    setEditingPolicy(policy);
  };

  const handleEditSave = () => {
    queryClient.invalidateQueries({ queryKey: ['policies'] });
    setEditingPolicy(null);
  };

  const handleDelete = (policy: Policy) => {
    setDeleteConfirm(policy);
  };

  const confirmDelete = () => {
    if (deleteConfirm) {
      deleteMutation.mutate(deleteConfirm.id);
    }
  };

  const exportPolicies = () => {
    if (!policyData?.policies) return;

    const data = JSON.stringify(policyData.policies, null, 2);
    const blob = new Blob([data], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'policies-export.json';
    a.click();
    URL.revokeObjectURL(url);
  };

  const policies = policyData?.policies || [];

  return (
    <div className="p-6 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold flex items-center gap-2">
            <Shield className="h-6 w-6 text-primary" />
            Policies
          </h1>
          <p className="text-muted-foreground mt-1">
            Manage cloud IAM policies and Keycloak RBAC mappings
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => refetchPolicies()}
            className="flex items-center gap-2 px-3 py-2 bg-muted hover:bg-muted/80 rounded"
            disabled={policiesLoading}
          >
            <RefreshCw className={`h-4 w-4 ${policiesLoading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={exportPolicies}
            className="flex items-center gap-2 px-3 py-2 bg-primary text-primary-foreground rounded hover:bg-primary/90"
            disabled={policies.length === 0}
          >
            <Download className="h-4 w-4" />
            Export
          </button>
        </div>
      </div>

      {/* Summary Stats */}
      {summary && (
        <div className="grid gap-4 md:grid-cols-5 mb-6">
          <div className="card-stat">
            <p className="card-stat-label">Total Policies</p>
            <p className="card-stat-value">{summary.total_count}</p>
          </div>
          <div className="card-stat">
            <p className="card-stat-label">IAM Policies</p>
            <p className="card-stat-value">{summary.by_type?.iam || 0}</p>
          </div>
          <div className="card-stat">
            <p className="card-stat-label">Resource Policies</p>
            <p className="card-stat-value">{summary.by_type?.resource || 0}</p>
          </div>
          <div className="card-stat">
            <p className="card-stat-label">Network Policies</p>
            <p className="card-stat-value">{summary.by_type?.network || 0}</p>
          </div>
          <div className="card-stat">
            <p className="card-stat-label">With Warnings</p>
            <p className="card-stat-value text-yellow-600">{summary.with_warnings}</p>
          </div>
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-4 border-b border-border mb-6">
        <button
          onClick={() => setActiveTab('list')}
          className={`flex items-center gap-2 px-4 py-2 border-b-2 -mb-px transition-colors ${
            activeTab === 'list'
              ? 'border-primary text-primary'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          }`}
        >
          <Shield className="h-4 w-4" />
          All Policies
        </button>
        <button
          onClick={() => setActiveTab('rbac')}
          className={`flex items-center gap-2 px-4 py-2 border-b-2 -mb-px transition-colors ${
            activeTab === 'rbac'
              ? 'border-primary text-primary'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          }`}
        >
          <Key className="h-4 w-4" />
          RBAC Mapping
        </button>
      </div>

      {/* Error State */}
      {policiesError && (
        <div className="alert-error mb-6">
          <AlertTriangle className="h-5 w-5" />
          <span>Failed to load policies: {policiesError.message}</span>
        </div>
      )}

      {/* Content */}
      {activeTab === 'list' ? (
        <PolicyList
          policies={policies}
          onEdit={handleEdit}
          onDelete={handleDelete}
          loading={policiesLoading}
        />
      ) : (
        <RBACMapper policies={policies} />
      )}

      {/* Edit Modal */}
      {editingPolicy && (
        <PolicyEditor
          policy={editingPolicy}
          onSave={handleEditSave}
          onCancel={() => setEditingPolicy(null)}
        />
      )}

      {/* Delete Confirmation Modal */}
      {deleteConfirm && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-background rounded-lg shadow-xl max-w-md w-full p-6">
            <div className="flex items-center gap-3 mb-4">
              <div className="p-2 bg-red-100 rounded-full">
                <Trash2 className="h-6 w-6 text-red-600" />
              </div>
              <div>
                <h3 className="font-semibold">Delete Policy</h3>
                <p className="text-sm text-muted-foreground">This action cannot be undone.</p>
              </div>
            </div>

            <p className="text-sm mb-6">
              Are you sure you want to delete the policy <strong>{deleteConfirm.name}</strong>?
            </p>

            <div className="flex justify-end gap-2">
              <button
                onClick={() => setDeleteConfirm(null)}
                className="px-4 py-2 bg-muted hover:bg-muted/80 rounded"
              >
                Cancel
              </button>
              <button
                onClick={confirmDelete}
                disabled={deleteMutation.isPending}
                className="px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700 disabled:opacity-50"
              >
                {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
