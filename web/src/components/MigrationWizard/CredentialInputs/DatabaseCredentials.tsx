import { useState, useMemo } from 'react';
import { Eye, EyeOff, RefreshCw } from 'lucide-react';

export interface DatabaseCredentialsProps {
  host: string;
  port: number;
  username: string;
  password: string;
  database: string;
  sslMode?: string;
  onChange: (field: string, value: string | number) => void;
  databaseType?: 'postgresql' | 'mysql';
  disabled?: boolean;
  onTestConnection?: () => Promise<boolean>;
  testingConnection?: boolean;
}

const DEFAULT_PORTS: Record<string, number> = {
  postgresql: 5432,
  mysql: 3306,
};

const SSL_MODES = [
  { value: 'disable', label: 'Disable' },
  { value: 'require', label: 'Require' },
  { value: 'verify-ca', label: 'Verify CA' },
  { value: 'verify-full', label: 'Verify Full' },
];

export function DatabaseCredentials({
  host,
  port,
  username,
  password,
  database,
  sslMode = 'disable',
  onChange,
  databaseType = 'postgresql',
  disabled = false,
  onTestConnection,
  testingConnection = false,
}: DatabaseCredentialsProps) {
  const [showPassword, setShowPassword] = useState(false);

  const connectionString = useMemo(() => {
    const maskedPassword = password ? '********' : '';
    const protocol = databaseType === 'postgresql' ? 'postgresql' : 'mysql';
    const sslParam = sslMode !== 'disable' ? `?sslmode=${sslMode}` : '';

    if (!host || !username || !database) {
      return '';
    }

    return `${protocol}://${username}:${maskedPassword}@${host}:${port}/${database}${sslParam}`;
  }, [host, port, username, password, database, sslMode, databaseType]);

  const handleTestConnection = async () => {
    if (onTestConnection) {
      await onTestConnection();
    }
  };

  const inputClassName = `w-full border border-input rounded-md px-3 py-2 focus:outline-none focus:ring-2 focus-visible:ring-ring focus:border-transparent disabled:bg-muted disabled:cursor-not-allowed`;

  return (
    <div className="space-y-4">
      {/* Host and Port - Two column grid */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label htmlFor="db-host" className="block text-sm font-medium text-foreground mb-1">
            Host <span className="text-error">*</span>
          </label>
          <input
            id="db-host"
            type="text"
            value={host}
            onChange={(e) => onChange('host', e.target.value)}
            placeholder="localhost or hostname"
            className={inputClassName}
            disabled={disabled}
            required
          />
        </div>
        <div>
          <label htmlFor="db-port" className="block text-sm font-medium text-foreground mb-1">
            Port <span className="text-error">*</span>
          </label>
          <input
            id="db-port"
            type="number"
            value={port || DEFAULT_PORTS[databaseType]}
            onChange={(e) => onChange('port', parseInt(e.target.value, 10) || DEFAULT_PORTS[databaseType])}
            placeholder={String(DEFAULT_PORTS[databaseType])}
            className={inputClassName}
            disabled={disabled}
            required
            min={1}
            max={65535}
          />
        </div>
      </div>

      {/* Username and Password - Two column grid */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label htmlFor="db-username" className="block text-sm font-medium text-foreground mb-1">
            Username <span className="text-error">*</span>
          </label>
          <input
            id="db-username"
            type="text"
            value={username}
            onChange={(e) => onChange('username', e.target.value)}
            placeholder="Database username"
            className={inputClassName}
            disabled={disabled}
            required
          />
        </div>
        <div>
          <label htmlFor="db-password" className="block text-sm font-medium text-foreground mb-1">
            Password <span className="text-error">*</span>
          </label>
          <div className="relative">
            <input
              id="db-password"
              type={showPassword ? 'text' : 'password'}
              value={password}
              onChange={(e) => onChange('password', e.target.value)}
              placeholder="Database password"
              className={`${inputClassName} pr-10`}
              disabled={disabled}
              required
            />
            <button
              type="button"
              onClick={() => setShowPassword(!showPassword)}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-foreground focus:outline-none"
              disabled={disabled}
              aria-label={showPassword ? 'Hide password' : 'Show password'}
            >
              {showPassword ? (
                <EyeOff className="h-5 w-5" />
              ) : (
                <Eye className="h-5 w-5" />
              )}
            </button>
          </div>
        </div>
      </div>

      {/* Database name - Full width */}
      <div>
        <label htmlFor="db-database" className="block text-sm font-medium text-foreground mb-1">
          Database Name <span className="text-error">*</span>
        </label>
        <input
          id="db-database"
          type="text"
          value={database}
          onChange={(e) => onChange('database', e.target.value)}
          placeholder="Database name"
          className={inputClassName}
          disabled={disabled}
          required
        />
      </div>

      {/* SSL Mode - Full width */}
      <div>
        <label htmlFor="db-ssl-mode" className="block text-sm font-medium text-foreground mb-1">
          SSL Mode
        </label>
        <select
          id="db-ssl-mode"
          value={sslMode}
          onChange={(e) => onChange('sslMode', e.target.value)}
          className={inputClassName}
          disabled={disabled}
        >
          {SSL_MODES.map((mode) => (
            <option key={mode.value} value={mode.value}>
              {mode.label}
            </option>
          ))}
        </select>
      </div>

      {/* Connection String Preview */}
      {connectionString && (
        <div>
          <label className="block text-sm font-medium text-foreground mb-1">
            Connection String Preview
          </label>
          <div className="bg-muted border border-border rounded-md px-3 py-2 font-mono text-sm text-muted-foreground break-all">
            {connectionString}
          </div>
        </div>
      )}

      {/* Test Connection Button */}
      {onTestConnection && (
        <div className="pt-2">
          <button
            type="button"
            onClick={handleTestConnection}
            disabled={disabled || testingConnection || !host || !username || !password || !database}
            className="inline-flex items-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-white hover:bg-muted focus:outline-none focus:ring-2 focus-visible:ring-ring focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <RefreshCw className={`h-4 w-4 mr-2 ${testingConnection ? 'animate-spin' : ''}`} />
            {testingConnection ? 'Testing...' : 'Test Connection'}
          </button>
        </div>
      )}
    </div>
  );
}

export default DatabaseCredentials;
