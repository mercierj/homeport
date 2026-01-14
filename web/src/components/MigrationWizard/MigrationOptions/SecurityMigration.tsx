import { useState, useMemo } from 'react';
import { Shield, Lock, Key, KeyRound, AlertTriangle, Info, Clock } from 'lucide-react';
import { ServiceMigrationCard } from '../ServiceMigrationCard';
import { useMigrationConfigStore } from '@/stores/migration-config';
import type { AuthConfig, SecretsConfig } from '../types';
import type { Resource } from '@/lib/migrate-api';

// Resource type matchers for filtering which cards to show
const COGNITO_TYPES = ['aws_cognito_user_pool', 'google_identity_platform_config', 'azurerm_active_directory_b2c'];
const IAM_TYPES = ['aws_iam_role', 'aws_iam_policy', 'google_project_iam_binding', 'azurerm_role_assignment'];
const SECRETS_TYPES = ['aws_secretsmanager_secret', 'aws_ssm_parameter', 'google_secret_manager_secret', 'azurerm_key_vault_secret'];
const KMS_TYPES = ['aws_kms_key', 'google_kms_crypto_key', 'azurerm_key_vault_key'];

// ============================================================================
// Helper Components
// ============================================================================

interface FormLabelProps {
  label: string;
  htmlFor?: string;
  required?: boolean;
  children: React.ReactNode;
}

function FormField({ label, htmlFor, required, children }: FormLabelProps) {
  return (
    <div className="space-y-1">
      <label
        htmlFor={htmlFor}
        className="block text-sm font-medium text-gray-700"
      >
        {label}
        {required && <span className="text-error ml-1">*</span>}
      </label>
      {children}
    </div>
  );
}

interface WarningBoxProps {
  children: React.ReactNode;
}

function WarningBox({ children }: WarningBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-warning/10 border border-warning/50 rounded-md">
      <AlertTriangle className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
      <span className="text-sm text-warning">{children}</span>
    </div>
  );
}

interface InfoBoxProps {
  children: React.ReactNode;
}

function InfoBox({ children }: InfoBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-info/10 border border-info/50 rounded-md">
      <Info className="w-4 h-4 text-info flex-shrink-0 mt-0.5" />
      <span className="text-sm text-info">{children}</span>
    </div>
  );
}

// ============================================================================
// Coming Soon Placeholder Component
// ============================================================================

interface ComingSoonProps {
  serviceName: string;
  description?: string;
}

function ComingSoon({ serviceName, description }: ComingSoonProps) {
  return (
    <div className="flex flex-col items-center justify-center py-8 text-center">
      <div className="w-12 h-12 rounded-full bg-muted flex items-center justify-center mb-3">
        <Clock className="w-6 h-6 text-muted-foreground/60" />
      </div>
      <p className="text-sm font-medium text-gray-600">Coming Soon</p>
      <p className="text-xs text-muted-foreground/60 mt-1">
        {description || `${serviceName} migration support is under development`}
      </p>
    </div>
  );
}

// ============================================================================
// Cognito Migration Section
// ============================================================================

interface CognitoMigrationSectionProps {
  config: AuthConfig;
  onConfigChange: (config: Partial<AuthConfig>) => void;
}

function CognitoMigrationSection({ config, onConfigChange }: CognitoMigrationSectionProps) {
  return (
    <div className="space-y-4">
      {/* User Pool ID */}
      <FormField label="User Pool ID" htmlFor="cognito-user-pool-id" required>
        <input
          id="cognito-user-pool-id"
          type="text"
          value={config.userPoolId}
          onChange={(e) => onConfigChange({ userPoolId: e.target.value })}
          placeholder="us-east-1_AbCdEfGhI"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
      </FormField>

      {/* Migration Options Checkboxes */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.migrateUsers}
            onChange={(e) => onConfigChange({ migrateUsers: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Users</span>
            <p className="text-xs text-gray-500">
              Import user accounts from Cognito
            </p>
          </div>
        </label>

        {config.migrateUsers && (
          <WarningBox>
            User passwords are hashed and cannot be directly migrated. Users will need to reset
            their passwords or use the password policy configured below.
          </WarningBox>
        )}

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.migrateGroups}
            onChange={(e) => onConfigChange({ migrateGroups: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Groups</span>
            <p className="text-xs text-gray-500">
              Import user groups and memberships
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.migrateRoles}
            onChange={(e) => onConfigChange({ migrateRoles: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Roles</span>
            <p className="text-xs text-gray-500">
              Import role configurations and assignments
            </p>
          </div>
        </label>
      </div>

      {/* Password Policy */}
      <FormField label="Password Policy" htmlFor="cognito-password-policy">
        <select
          id="cognito-password-policy"
          value={config.passwordPolicy}
          onChange={(e) =>
            onConfigChange({ passwordPolicy: e.target.value as AuthConfig['passwordPolicy'] })
          }
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        >
          <option value="reset">Reset all passwords</option>
          <option value="preserve-hash">Preserve hashes (requires custom provider)</option>
          <option value="send-reset-email">Send reset emails to users</option>
        </select>
        <p className="text-xs text-gray-500 mt-1">
          {config.passwordPolicy === 'reset' &&
            'Users will be required to set new passwords on first login.'}
          {config.passwordPolicy === 'preserve-hash' &&
            'Requires a custom password provider that supports Cognito hash format.'}
          {config.passwordPolicy === 'send-reset-email' &&
            'Users will receive an email to reset their password after migration.'}
        </p>
      </FormField>

      {/* MFA Handling */}
      <FormField label="MFA Handling" htmlFor="cognito-mfa-handling">
        <select
          id="cognito-mfa-handling"
          value={config.mfaHandling}
          onChange={(e) =>
            onConfigChange({ mfaHandling: e.target.value as AuthConfig['mfaHandling'] })
          }
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        >
          <option value="disable">Disable MFA</option>
          <option value="preserve">Preserve (if compatible)</option>
          <option value="require-reconfigure">Require reconfiguration</option>
        </select>
        <p className="text-xs text-gray-500 mt-1">
          {config.mfaHandling === 'disable' && 'MFA will be disabled for all migrated users.'}
          {config.mfaHandling === 'preserve' &&
            'Attempt to preserve MFA settings if Keycloak supports the same method.'}
          {config.mfaHandling === 'require-reconfigure' &&
            'Users will be required to set up MFA again after migration.'}
        </p>
      </FormField>
    </div>
  );
}

// ============================================================================
// IAM Migration Section
// ============================================================================

interface IAMConfig {
  enabled: boolean;
  roleArns: string;
  generateRbacMappings: boolean;
  exportFormat: 'json' | 'yaml';
}

interface IAMMigrationSectionProps {
  config: IAMConfig;
  onConfigChange: (config: Partial<IAMConfig>) => void;
}

function IAMMigrationSection({ config, onConfigChange }: IAMMigrationSectionProps) {
  return (
    <div className="space-y-4">
      <InfoBox>
        IAM roles and policies will be analyzed and converted to local RBAC configurations.
        Manual review is required after export to ensure proper permission mappings.
      </InfoBox>

      {/* Role ARNs */}
      <FormField label="Role ARNs" htmlFor="iam-role-arns">
        <textarea
          id="iam-role-arns"
          value={config.roleArns}
          onChange={(e) => onConfigChange({ roleArns: e.target.value })}
          placeholder="arn:aws:iam::123456789012:role/MyRole, arn:aws:iam::123456789012:role/AnotherRole"
          rows={3}
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-none"
        />
        <p className="text-xs text-gray-500 mt-1">
          Enter IAM role ARNs separated by commas. Leave empty to export all roles.
        </p>
      </FormField>

      {/* Generate RBAC Mappings */}
      <label className="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          checked={config.generateRbacMappings}
          onChange={(e) => onConfigChange({ generateRbacMappings: e.target.checked })}
          className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
        />
        <div>
          <span className="text-sm font-medium text-gray-700">Generate RBAC Mappings</span>
          <p className="text-xs text-gray-500">
            Automatically create suggested role-based access control mappings
          </p>
        </div>
      </label>

      {/* Export Format */}
      <FormField label="Export Format" htmlFor="iam-export-format">
        <div className="flex gap-4">
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              name="iam-export-format"
              value="json"
              checked={config.exportFormat === 'json'}
              onChange={() => onConfigChange({ exportFormat: 'json' })}
              className="w-4 h-4 text-primary border-input focus:ring-blue-500"
            />
            <span className="text-sm text-gray-700">JSON</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              name="iam-export-format"
              value="yaml"
              checked={config.exportFormat === 'yaml'}
              onChange={() => onConfigChange({ exportFormat: 'yaml' })}
              className="w-4 h-4 text-primary border-input focus:ring-blue-500"
            />
            <span className="text-sm text-gray-700">YAML</span>
          </label>
        </div>
      </FormField>

      <WarningBox>
        Exported IAM configurations require manual review. Some AWS-specific permissions
        may not have direct equivalents and will need custom handling.
      </WarningBox>
    </div>
  );
}

// ============================================================================
// Secrets Manager Migration Section
// ============================================================================

interface SecretsManagerMigrationSectionProps {
  config: SecretsConfig;
  onConfigChange: (config: Partial<SecretsConfig>) => void;
}

function SecretsManagerMigrationSection({
  config,
  onConfigChange,
}: SecretsManagerMigrationSectionProps) {
  const [secretPathsInput, setSecretPathsInput] = useState(config.secretPaths.join(', '));

  const handleSecretPathsChange = (value: string) => {
    setSecretPathsInput(value);
    const paths = value
      .split(',')
      .map((p) => p.trim())
      .filter((p) => p.length > 0);
    onConfigChange({ secretPaths: paths });
  };

  return (
    <div className="space-y-4">
      {/* Secret Paths */}
      <FormField label="Secret Paths" htmlFor="secrets-paths" required>
        <textarea
          id="secrets-paths"
          value={secretPathsInput}
          onChange={(e) => handleSecretPathsChange(e.target.value)}
          placeholder="production/database, production/api-keys, /* for all"
          rows={3}
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-none"
        />
        <p className="text-xs text-gray-500 mt-1">
          Enter secret paths separated by commas. Use /* to migrate all secrets.
        </p>
      </FormField>

      {/* Target Vault Path */}
      <FormField label="Target Vault Path" htmlFor="secrets-target-path" required>
        <input
          id="secrets-target-path"
          type="text"
          value={config.targetPath}
          onChange={(e) => onConfigChange({ targetPath: e.target.value })}
          placeholder="/secrets/aws-migrated"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          Base path in Vault where secrets will be stored
        </p>
      </FormField>

      {/* Enable Encryption */}
      <label className="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          checked={config.encryption}
          onChange={(e) => onConfigChange({ encryption: e.target.checked })}
          className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
        />
        <div>
          <span className="text-sm font-medium text-gray-700">Enable Encryption</span>
          <p className="text-xs text-gray-500">
            Encrypt secrets at rest using Vault encryption engine
          </p>
        </div>
      </label>

      <WarningBox>
        Secret access will be logged during migration. Ensure you have appropriate audit
        controls in place and review access logs after the migration is complete.
      </WarningBox>
    </div>
  );
}

// ============================================================================
// Main SecurityMigration Component
// ============================================================================

interface SecurityMigrationProps {
  resources: Resource[];
  filter?: 'auth' | 'secrets' | 'all';
}

export function SecurityMigration({ resources = [], filter = 'all' }: SecurityMigrationProps) {
  const { auth, secrets, setAuthConfig, setSecretsConfig } = useMigrationConfigStore();

  // Determine which service cards to show based on discovered resources AND filter
  const { hasCognito, hasIAM, hasSecrets, hasKMS } = useMemo(() => {
    const showAuth = filter === 'all' || filter === 'auth';
    const showSecrets = filter === 'all' || filter === 'secrets';

    return {
      hasCognito: showAuth && resources.some(r => COGNITO_TYPES.includes(r.type)),
      hasIAM: showAuth && resources.some(r => IAM_TYPES.includes(r.type)),
      hasSecrets: showSecrets && resources.some(r => SECRETS_TYPES.includes(r.type)),
      hasKMS: showSecrets && resources.some(r => KMS_TYPES.includes(r.type)),
    };
  }, [resources, filter]);

  // Local state for IAM config (since it's not part of main types)
  const [iamConfig, setIamConfig] = useState<IAMConfig>({
    enabled: false,
    roleArns: '',
    generateRbacMappings: true,
    exportFormat: 'json',
  });

  // Local state for KMS enabled
  const [kmsEnabled, setKmsEnabled] = useState(false);

  const handleIamChange = (config: Partial<IAMConfig>) => {
    setIamConfig((prev) => ({ ...prev, ...config }));
  };

  // If no security resources discovered, show empty state with context-aware message
  if (!hasCognito && !hasIAM && !hasSecrets && !hasKMS) {
    const emptyMessage = filter === 'auth'
      ? 'Cognito or IAM resources will appear here when detected.'
      : filter === 'secrets'
      ? 'Secrets Manager or KMS resources will appear here when detected.'
      : 'Cognito, IAM, Secrets Manager, or KMS resources will appear here when detected.';

    const emptyTitle = filter === 'auth'
      ? 'No authentication resources discovered'
      : filter === 'secrets'
      ? 'No secrets resources discovered'
      : 'No security resources discovered';

    return (
      <div className="text-center py-8 text-muted-foreground">
        <Shield className="w-12 h-12 mx-auto mb-3 opacity-50" />
        <p className="font-medium">{emptyTitle}</p>
        <p className="text-sm mt-1">{emptyMessage}</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Cognito to Keycloak Section - only show if Cognito resources discovered */}
      {hasCognito && (
        <ServiceMigrationCard
          title="Cognito → Keycloak"
          description="Migrate user pools and identity"
          icon={Shield}
          enabled={auth.enabled}
          onToggle={(enabled) => setAuthConfig({ enabled })}
          defaultExpanded={true}
        >
          <CognitoMigrationSection config={auth} onConfigChange={setAuthConfig} />
        </ServiceMigrationCard>
      )}

      {/* IAM to Local RBAC Section - only show if IAM resources discovered */}
      {hasIAM && (
        <ServiceMigrationCard
          title="IAM → Local RBAC"
          description="Convert IAM roles to local permissions"
          icon={Lock}
          enabled={iamConfig.enabled}
          onToggle={(enabled) => handleIamChange({ enabled })}
          defaultExpanded={true}
        >
          <IAMMigrationSection config={iamConfig} onConfigChange={handleIamChange} />
        </ServiceMigrationCard>
      )}

      {/* Secrets Manager to HashiCorp Vault Section - only show if Secrets resources discovered */}
      {hasSecrets && (
        <ServiceMigrationCard
          title="Secrets Manager → HashiCorp Vault"
          description="Migrate secrets to Vault"
          icon={Key}
          enabled={secrets.enabled}
          onToggle={(enabled) => setSecretsConfig({ enabled })}
          defaultExpanded={true}
        >
          <SecretsManagerMigrationSection config={secrets} onConfigChange={setSecretsConfig} />
        </ServiceMigrationCard>
      )}

      {/* KMS to Local Encryption Section - only show if KMS resources discovered */}
      {hasKMS && (
        <ServiceMigrationCard
          title="KMS → Local Encryption"
          description="Export key policies and configurations"
          icon={KeyRound}
          enabled={kmsEnabled}
          onToggle={setKmsEnabled}
          defaultExpanded={true}
        >
          <ComingSoon
            serviceName="KMS"
            description="Encryption key migration is complex and requires careful planning. This feature is under development."
          />
        </ServiceMigrationCard>
      )}
    </div>
  );
}

export default SecurityMigration;
