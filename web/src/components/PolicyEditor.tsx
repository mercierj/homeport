import { useState } from 'react';
import { Plus, Trash2, X, Save, AlertCircle, CheckCircle } from 'lucide-react';
import type { Policy, Statement, NormalizedPolicy } from '@/lib/policy-types';
import { PREDEFINED_ACTIONS } from '@/lib/policy-types';
import { updatePolicy, validatePolicy } from '@/lib/policy-api';

interface PolicyEditorProps {
  policy: Policy;
  onSave: (policy: Policy) => void;
  onCancel: () => void;
}

export function PolicyEditor({ policy, onSave, onCancel }: PolicyEditorProps) {
  const [statements, setStatements] = useState<Statement[]>(
    policy.normalized_policy?.statements || []
  );
  const [showRawJson, setShowRawJson] = useState(false);
  const [rawJson, setRawJson] = useState(JSON.stringify(policy.original_document, null, 2));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validationErrors, setValidationErrors] = useState<string[]>([]);

  const addStatement = () => {
    setStatements([
      ...statements,
      {
        effect: 'Allow',
        principals: [],
        actions: [],
        resources: [],
      },
    ]);
  };

  const removeStatement = (index: number) => {
    setStatements(statements.filter((_, i) => i !== index));
  };

  const updateStatement = (index: number, updates: Partial<Statement>) => {
    setStatements(
      statements.map((stmt, i) => (i === index ? { ...stmt, ...updates } : stmt))
    );
  };

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    setValidationErrors([]);

    try {
      let normalizedPolicy: NormalizedPolicy;
      let originalDocument: unknown;

      if (showRawJson) {
        // Parse raw JSON
        originalDocument = JSON.parse(rawJson);
        normalizedPolicy = policy.normalized_policy || { statements: [] };
      } else {
        // Use statement editor
        normalizedPolicy = { statements };
        originalDocument = policy.original_document;
      }

      const updated = await updatePolicy(policy.id, {
        normalized_policy: normalizedPolicy,
        original_document: originalDocument,
      });

      // Validate the updated policy
      const validation = await validatePolicy(policy.id);
      if (!validation.valid && validation.errors) {
        setValidationErrors(validation.errors.filter((e) => e.severe).map((e) => e.message));
        if (validation.errors.some((e) => e.severe)) {
          setError('Policy has validation errors');
          setSaving(false);
          return;
        }
      }

      onSave(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save policy');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-background rounded-lg shadow-xl max-w-4xl w-full max-h-[90vh] overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border">
          <h2 className="text-lg font-semibold">Edit Policy: {policy.name}</h2>
          <button onClick={onCancel} className="p-1 hover:bg-muted rounded">
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Content */}
        <div className="p-4 overflow-y-auto max-h-[calc(90vh-140px)]">
          {/* Mode Toggle */}
          <div className="flex gap-2 mb-4">
            <button
              onClick={() => setShowRawJson(false)}
              className={`px-3 py-1 rounded text-sm ${
                !showRawJson ? 'bg-primary text-primary-foreground' : 'bg-muted'
              }`}
            >
              Visual Editor
            </button>
            <button
              onClick={() => setShowRawJson(true)}
              className={`px-3 py-1 rounded text-sm ${
                showRawJson ? 'bg-primary text-primary-foreground' : 'bg-muted'
              }`}
            >
              Raw JSON
            </button>
          </div>

          {/* Error Display */}
          {error && (
            <div className="alert-error mb-4">
              <AlertCircle className="h-4 w-4" />
              <span>{error}</span>
            </div>
          )}

          {/* Validation Errors */}
          {validationErrors.length > 0 && (
            <div className="alert-warning mb-4">
              <AlertCircle className="h-4 w-4" />
              <div>
                <p className="font-medium">Validation Issues:</p>
                <ul className="list-disc list-inside">
                  {validationErrors.map((err, i) => (
                    <li key={i}>{err}</li>
                  ))}
                </ul>
              </div>
            </div>
          )}

          {showRawJson ? (
            /* Raw JSON Editor */
            <div>
              <label className="label">Policy Document (JSON)</label>
              <textarea
                value={rawJson}
                onChange={(e) => setRawJson(e.target.value)}
                className="textarea font-mono text-sm h-96"
                spellCheck={false}
              />
            </div>
          ) : (
            /* Statement Editor */
            <div className="space-y-4">
              {statements.map((stmt, index) => (
                <StatementEditor
                  key={index}
                  statement={stmt}
                  index={index}
                  onChange={(updates) => updateStatement(index, updates)}
                  onRemove={() => removeStatement(index)}
                />
              ))}

              <button
                onClick={addStatement}
                className="flex items-center gap-2 w-full justify-center py-2 border-2 border-dashed border-border rounded-lg hover:border-primary hover:text-primary"
              >
                <Plus className="h-4 w-4" />
                Add Statement
              </button>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 p-4 border-t border-border">
          <button
            onClick={onCancel}
            className="px-4 py-2 text-sm bg-muted hover:bg-muted/80 rounded"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="flex items-center gap-2 px-4 py-2 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90 disabled:opacity-50"
          >
            {saving ? (
              <>Saving...</>
            ) : (
              <>
                <Save className="h-4 w-4" />
                Save Changes
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

interface StatementEditorProps {
  statement: Statement;
  index: number;
  onChange: (updates: Partial<Statement>) => void;
  onRemove: () => void;
}

function StatementEditor({ statement, index, onChange, onRemove }: StatementEditorProps) {
  const [principalInput, setPrincipalInput] = useState('');
  const [actionInput, setActionInput] = useState('');
  const [resourceInput, setResourceInput] = useState('');

  const addPrincipal = () => {
    if (!principalInput) return;
    const [type, id] = principalInput.includes(':')
      ? principalInput.split(':')
      : ['user', principalInput];
    onChange({
      principals: [...(statement.principals || []), { type, id }],
    });
    setPrincipalInput('');
  };

  const removePrincipal = (i: number) => {
    onChange({
      principals: statement.principals?.filter((_, idx) => idx !== i),
    });
  };

  const toggleAction = (category: string, action: string) => {
    const fullAction = `${category}:${action}`;
    const hasAction = statement.actions.includes(fullAction);
    onChange({
      actions: hasAction
        ? statement.actions.filter((a) => a !== fullAction)
        : [...statement.actions, fullAction],
    });
  };

  const addAction = () => {
    if (!actionInput || statement.actions.includes(actionInput)) return;
    onChange({
      actions: [...statement.actions, actionInput],
    });
    setActionInput('');
  };

  const removeAction = (i: number) => {
    onChange({
      actions: statement.actions.filter((_, idx) => idx !== i),
    });
  };

  const addResource = () => {
    if (!resourceInput || statement.resources.includes(resourceInput)) return;
    onChange({
      resources: [...statement.resources, resourceInput],
    });
    setResourceInput('');
  };

  const removeResource = (i: number) => {
    onChange({
      resources: statement.resources.filter((_, idx) => idx !== i),
    });
  };

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center justify-between mb-4">
        <h4 className="font-medium">Statement {index + 1}</h4>
        <button onClick={onRemove} className="p-1 text-red-500 hover:bg-red-50 rounded">
          <Trash2 className="h-4 w-4" />
        </button>
      </div>

      <div className="space-y-4">
        {/* Effect Toggle */}
        <div>
          <label className="label">Effect</label>
          <div className="flex gap-2">
            <button
              onClick={() => onChange({ effect: 'Allow' })}
              className={`flex items-center gap-1 px-3 py-1 rounded text-sm ${
                statement.effect === 'Allow'
                  ? 'bg-green-500 text-white'
                  : 'bg-muted hover:bg-muted/80'
              }`}
            >
              <CheckCircle className="h-3 w-3" />
              Allow
            </button>
            <button
              onClick={() => onChange({ effect: 'Deny' })}
              className={`flex items-center gap-1 px-3 py-1 rounded text-sm ${
                statement.effect === 'Deny'
                  ? 'bg-red-500 text-white'
                  : 'bg-muted hover:bg-muted/80'
              }`}
            >
              <X className="h-3 w-3" />
              Deny
            </button>
          </div>
        </div>

        {/* Principals */}
        <div>
          <label className="label">Principals</label>
          <div className="flex gap-2 mb-2 flex-wrap">
            {statement.principals?.map((p, i) => (
              <span key={i} className="badge-outline flex items-center gap-1">
                {p.type}:{p.id}
                <button onClick={() => removePrincipal(i)} className="hover:text-red-500">
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            <input
              type="text"
              value={principalInput}
              onChange={(e) => setPrincipalInput(e.target.value)}
              placeholder="user:alice or role:admins"
              className="input flex-1"
              onKeyDown={(e) => e.key === 'Enter' && addPrincipal()}
            />
            <button onClick={addPrincipal} className="px-3 py-1 bg-primary text-primary-foreground rounded">
              <Plus className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Actions */}
        <div>
          <label className="label">Actions</label>
          <div className="grid grid-cols-2 gap-4 mb-3">
            {PREDEFINED_ACTIONS.map((category) => (
              <div key={category.name} className="border border-border rounded p-2">
                <p className="text-sm font-medium mb-2">{category.name}</p>
                <div className="flex flex-wrap gap-1">
                  {category.actions.map((action) => {
                    const fullAction = `${category.name}:${action}`;
                    const isSelected = statement.actions.includes(fullAction);
                    return (
                      <button
                        key={action}
                        onClick={() => toggleAction(category.name, action)}
                        className={`text-xs px-2 py-0.5 rounded ${
                          isSelected
                            ? 'bg-primary text-primary-foreground'
                            : 'bg-muted hover:bg-muted/80'
                        }`}
                      >
                        {action}
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
          <div className="flex gap-2 mb-2 flex-wrap">
            {statement.actions
              .filter((a) => !PREDEFINED_ACTIONS.some((c) => a.startsWith(`${c.name}:`)))
              .map((action, i) => (
                <span key={i} className="badge-outline flex items-center gap-1">
                  {action}
                  <button onClick={() => removeAction(statement.actions.indexOf(action))} className="hover:text-red-500">
                    <X className="h-3 w-3" />
                  </button>
                </span>
              ))}
          </div>
          <div className="flex gap-2">
            <input
              type="text"
              value={actionInput}
              onChange={(e) => setActionInput(e.target.value)}
              placeholder="Custom action (e.g., s3:GetObject)"
              className="input flex-1"
              onKeyDown={(e) => e.key === 'Enter' && addAction()}
            />
            <button onClick={addAction} className="px-3 py-1 bg-primary text-primary-foreground rounded">
              <Plus className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Resources */}
        <div>
          <label className="label">Resources</label>
          <div className="flex gap-2 mb-2 flex-wrap">
            {statement.resources.map((resource, i) => (
              <span key={i} className="badge-outline flex items-center gap-1">
                {resource}
                <button onClick={() => removeResource(i)} className="hover:text-red-500">
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            <input
              type="text"
              value={resourceInput}
              onChange={(e) => setResourceInput(e.target.value)}
              placeholder="bucket/my-bucket/* or *"
              className="input flex-1"
              onKeyDown={(e) => e.key === 'Enter' && addResource()}
            />
            <button onClick={addResource} className="px-3 py-1 bg-primary text-primary-foreground rounded">
              <Plus className="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
