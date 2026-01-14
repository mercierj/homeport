import { useState } from 'react';
import { Eye, EyeOff, Shield, Key } from 'lucide-react';

export interface SecurityCredentialsProps {
  targetType: 'vault' | 'keycloak';
  vaultAddr?: string;
  vaultToken?: string;
  vaultNamespace?: string;
  keycloakUrl?: string;
  keycloakRealm?: string;
  keycloakAdminUser?: string;
  keycloakAdminPassword?: string;
  onChange: (field: string, value: string) => void;
  disabled?: boolean;
}

interface FormFieldProps {
  label: string;
  htmlFor?: string;
  required?: boolean;
  children: React.ReactNode;
}

function FormField({ label, htmlFor, required, children }: FormFieldProps) {
  return (
    <div className="space-y-1">
      <label
        htmlFor={htmlFor}
        className="block text-sm font-medium text-foreground"
      >
        {label}
        {required && <span className="text-error ml-1">*</span>}
      </label>
      {children}
    </div>
  );
}

interface VaultCredentialsFormProps {
  vaultAddr: string;
  vaultToken: string;
  vaultNamespace: string;
  onChange: (field: string, value: string) => void;
  disabled: boolean;
}

function VaultCredentialsForm({
  vaultAddr,
  vaultToken,
  vaultNamespace,
  onChange,
  disabled,
}: VaultCredentialsFormProps) {
  const [showToken, setShowToken] = useState(false);

  const inputClassName = `w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus-visible:ring-ring focus:border-blue-500 ${
    disabled ? 'bg-muted cursor-not-allowed' : ''
  }`;

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2 pb-2 border-b border-border">
        <Shield className="w-5 h-5 text-primary" />
        <h4 className="text-sm font-medium text-gray-800">HashiCorp Vault Configuration</h4>
      </div>

      {/* Vault Address */}
      <FormField label="Vault Address" htmlFor="vault-addr" required>
        <input
          id="vault-addr"
          type="text"
          value={vaultAddr}
          onChange={(e) => onChange('vaultAddr', e.target.value)}
          placeholder="https://vault.example.com:8200"
          className={inputClassName}
          disabled={disabled}
        />
        <p className="text-xs text-gray-500 mt-1">
          Full URL to your Vault server including port
        </p>
      </FormField>

      {/* Vault Token */}
      <FormField label="Vault Token" htmlFor="vault-token" required>
        <div className="relative">
          <input
            id="vault-token"
            type={showToken ? 'text' : 'password'}
            value={vaultToken}
            onChange={(e) => onChange('vaultToken', e.target.value)}
            placeholder="hvs.XXXXXXXXXXXXXXXXXXXX"
            className={`${inputClassName} pr-10`}
            disabled={disabled}
          />
          <button
            type="button"
            onClick={() => setShowToken(!showToken)}
            className="absolute inset-y-0 right-0 flex items-center pr-3 text-muted-foreground/60 hover:text-muted-foreground"
            disabled={disabled}
            tabIndex={-1}
          >
            {showToken ? (
              <EyeOff className="w-4 h-4" />
            ) : (
              <Eye className="w-4 h-4" />
            )}
          </button>
        </div>
        <p className="text-xs text-gray-500 mt-1">
          Authentication token with appropriate permissions
        </p>
      </FormField>

      {/* Vault Namespace (optional) */}
      <FormField label="Namespace" htmlFor="vault-namespace">
        <input
          id="vault-namespace"
          type="text"
          value={vaultNamespace}
          onChange={(e) => onChange('vaultNamespace', e.target.value)}
          placeholder="admin/my-namespace (optional)"
          className={inputClassName}
          disabled={disabled}
        />
        <p className="text-xs text-gray-500 mt-1">
          Enterprise namespace (leave empty for root namespace)
        </p>
      </FormField>
    </div>
  );
}

interface KeycloakCredentialsFormProps {
  keycloakUrl: string;
  keycloakRealm: string;
  keycloakAdminUser: string;
  keycloakAdminPassword: string;
  onChange: (field: string, value: string) => void;
  disabled: boolean;
}

function KeycloakCredentialsForm({
  keycloakUrl,
  keycloakRealm,
  keycloakAdminUser,
  keycloakAdminPassword,
  onChange,
  disabled,
}: KeycloakCredentialsFormProps) {
  const [showPassword, setShowPassword] = useState(false);

  const inputClassName = `w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus-visible:ring-ring focus:border-blue-500 ${
    disabled ? 'bg-muted cursor-not-allowed' : ''
  }`;

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2 pb-2 border-b border-border">
        <Key className="w-5 h-5 text-orange-600" />
        <h4 className="text-sm font-medium text-gray-800">Keycloak Configuration</h4>
      </div>

      {/* Keycloak URL */}
      <FormField label="Keycloak URL" htmlFor="keycloak-url" required>
        <input
          id="keycloak-url"
          type="text"
          value={keycloakUrl}
          onChange={(e) => onChange('keycloakUrl', e.target.value)}
          placeholder="https://keycloak.example.com"
          className={inputClassName}
          disabled={disabled}
        />
        <p className="text-xs text-gray-500 mt-1">
          Base URL of your Keycloak server
        </p>
      </FormField>

      {/* Realm */}
      <FormField label="Realm" htmlFor="keycloak-realm" required>
        <input
          id="keycloak-realm"
          type="text"
          value={keycloakRealm}
          onChange={(e) => onChange('keycloakRealm', e.target.value)}
          placeholder="master"
          className={inputClassName}
          disabled={disabled}
        />
        <p className="text-xs text-gray-500 mt-1">
          The Keycloak realm to authenticate against
        </p>
      </FormField>

      {/* Admin Username */}
      <FormField label="Admin Username" htmlFor="keycloak-admin-user" required>
        <input
          id="keycloak-admin-user"
          type="text"
          value={keycloakAdminUser}
          onChange={(e) => onChange('keycloakAdminUser', e.target.value)}
          placeholder="admin"
          className={inputClassName}
          disabled={disabled}
        />
        <p className="text-xs text-gray-500 mt-1">
          Administrator username with realm management permissions
        </p>
      </FormField>

      {/* Admin Password */}
      <FormField label="Admin Password" htmlFor="keycloak-admin-password" required>
        <div className="relative">
          <input
            id="keycloak-admin-password"
            type={showPassword ? 'text' : 'password'}
            value={keycloakAdminPassword}
            onChange={(e) => onChange('keycloakAdminPassword', e.target.value)}
            placeholder="Enter admin password"
            className={`${inputClassName} pr-10`}
            disabled={disabled}
          />
          <button
            type="button"
            onClick={() => setShowPassword(!showPassword)}
            className="absolute inset-y-0 right-0 flex items-center pr-3 text-muted-foreground/60 hover:text-muted-foreground"
            disabled={disabled}
            tabIndex={-1}
          >
            {showPassword ? (
              <EyeOff className="w-4 h-4" />
            ) : (
              <Eye className="w-4 h-4" />
            )}
          </button>
        </div>
      </FormField>
    </div>
  );
}

export function SecurityCredentials({
  targetType,
  vaultAddr = '',
  vaultToken = '',
  vaultNamespace = '',
  keycloakUrl = '',
  keycloakRealm = '',
  keycloakAdminUser = '',
  keycloakAdminPassword = '',
  onChange,
  disabled = false,
}: SecurityCredentialsProps) {
  if (targetType === 'vault') {
    return (
      <VaultCredentialsForm
        vaultAddr={vaultAddr}
        vaultToken={vaultToken}
        vaultNamespace={vaultNamespace}
        onChange={onChange}
        disabled={disabled}
      />
    );
  }

  return (
    <KeycloakCredentialsForm
      keycloakUrl={keycloakUrl}
      keycloakRealm={keycloakRealm}
      keycloakAdminUser={keycloakAdminUser}
      keycloakAdminPassword={keycloakAdminPassword}
      onChange={onChange}
      disabled={disabled}
    />
  );
}

export default SecurityCredentials;
