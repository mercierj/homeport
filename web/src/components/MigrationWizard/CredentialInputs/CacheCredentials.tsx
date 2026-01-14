import { useState } from 'react';
import { Eye, EyeOff } from 'lucide-react';

export interface CacheCredentialsProps {
  endpoint: string;
  port: number;
  authToken?: string;
  useTLS?: boolean;
  onChange: (field: string, value: string | number | boolean) => void;
  cacheType?: 'redis' | 'memcached';
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

export function CacheCredentials({
  endpoint,
  port,
  authToken = '',
  useTLS = false,
  onChange,
  cacheType = 'redis',
  disabled = false,
}: CacheCredentialsProps) {
  const [showAuthToken, setShowAuthToken] = useState(false);

  const defaultPort = cacheType === 'redis' ? 6379 : 11211;
  const displayPort = port || defaultPort;

  const inputClassName = `w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus-visible:ring-ring focus:border-blue-500 ${
    disabled ? 'bg-muted cursor-not-allowed' : ''
  }`;

  return (
    <div className="space-y-4">
      {/* Endpoint */}
      <FormField label="Endpoint" htmlFor="cache-endpoint" required>
        <input
          id="cache-endpoint"
          type="text"
          value={endpoint}
          onChange={(e) => onChange('endpoint', e.target.value)}
          placeholder={
            cacheType === 'redis'
              ? 'redis.example.com'
              : 'memcached.example.com'
          }
          className={inputClassName}
          disabled={disabled}
        />
        <p className="text-xs text-gray-500 mt-1">
          The hostname or IP address of your {cacheType === 'redis' ? 'Redis' : 'Memcached'} server
        </p>
      </FormField>

      {/* Port */}
      <FormField label="Port" htmlFor="cache-port" required>
        <input
          id="cache-port"
          type="number"
          min={1}
          max={65535}
          value={displayPort}
          onChange={(e) => {
            const value = parseInt(e.target.value, 10);
            if (!isNaN(value) && value >= 1 && value <= 65535) {
              onChange('port', value);
            }
          }}
          placeholder={String(defaultPort)}
          className={`w-32 px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus-visible:ring-ring focus:border-blue-500 ${
            disabled ? 'bg-muted cursor-not-allowed' : ''
          }`}
          disabled={disabled}
        />
        <p className="text-xs text-gray-500 mt-1">
          Default: {defaultPort} for {cacheType === 'redis' ? 'Redis' : 'Memcached'}
        </p>
      </FormField>

      {/* Auth Token (password with toggle) */}
      <FormField label="Auth Token" htmlFor="cache-auth-token">
        <div className="relative">
          <input
            id="cache-auth-token"
            type={showAuthToken ? 'text' : 'password'}
            value={authToken}
            onChange={(e) => onChange('authToken', e.target.value)}
            placeholder="Optional authentication token"
            className={`${inputClassName} pr-10`}
            disabled={disabled}
          />
          <button
            type="button"
            onClick={() => setShowAuthToken(!showAuthToken)}
            className="absolute inset-y-0 right-0 flex items-center pr-3 text-muted-foreground/60 hover:text-muted-foreground"
            disabled={disabled}
            tabIndex={-1}
          >
            {showAuthToken ? (
              <EyeOff className="w-4 h-4" />
            ) : (
              <Eye className="w-4 h-4" />
            )}
          </button>
        </div>
        <p className="text-xs text-gray-500 mt-1">
          {cacheType === 'redis'
            ? 'Redis AUTH password or token (optional)'
            : 'SASL authentication token (optional)'}
        </p>
      </FormField>

      {/* Use TLS Checkbox */}
      <label className="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          checked={useTLS}
          onChange={(e) => onChange('useTLS', e.target.checked)}
          className="w-4 h-4 text-primary rounded border-input focus-visible:ring-ring"
          disabled={disabled}
        />
        <div>
          <span className="text-sm font-medium text-foreground">Use TLS</span>
          <p className="text-xs text-gray-500">
            Enable TLS/SSL encryption for the connection
          </p>
        </div>
      </label>
    </div>
  );
}

export default CacheCredentials;
