import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import {
  RefreshCw,
  Plus,
  Trash2,
  Pencil,
  Loader2,
  Users,
  Shield,
  Clock,
  X,
  Check,
  UserCheck,
  UserX,
} from 'lucide-react';
import {
  listUsers,
  listRoles,
  listPermissions,
  createUser,
  updateUser,
  deleteUser,
  createRole,
  deleteRole,
  getUserStatusBadgeClass,
  formatLastLogin,
  type User,
  type CreateUserRequest,
  type UpdateUserRequest,
  type CreateRoleRequest,
} from '@/lib/identity-api';

interface IdentityManagerProps {
  onError?: (error: Error) => void;
}

type TabType = 'users' | 'roles';

export function IdentityManager({ onError }: IdentityManagerProps) {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<TabType>('users');

  // User form state
  const [showUserForm, setShowUserForm] = useState(false);
  const [editingUser, setEditingUser] = useState<User | null>(null);
  const [userFormData, setUserFormData] = useState({
    username: '',
    email: '',
    password: '',
    roles: [] as string[],
    enabled: true,
  });

  // Role form state
  const [showRoleForm, setShowRoleForm] = useState(false);
  const [roleFormData, setRoleFormData] = useState({
    name: '',
    description: '',
    permissions: [] as string[],
  });

  const [activeAction, setActiveAction] = useState<{ id: string; action: string } | null>(null);

  // Queries
  const usersQuery = useQuery({
    queryKey: ['identity', 'users'],
    queryFn: listUsers,
    refetchInterval: 30000,
  });

  const rolesQuery = useQuery({
    queryKey: ['identity', 'roles'],
    queryFn: listRoles,
    refetchInterval: 30000,
  });

  const permissionsQuery = useQuery({
    queryKey: ['identity', 'permissions'],
    queryFn: listPermissions,
  });

  const clearActiveAction = () => setActiveAction(null);

  // User mutations
  const createUserMutation = useMutation({
    mutationFn: (req: CreateUserRequest) => createUser(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identity', 'users'] });
      resetUserForm();
    },
    onError: (err: Error) => onError?.(err),
  });

  const updateUserMutation = useMutation({
    mutationFn: ({ id, req }: { id: string; req: UpdateUserRequest }) => updateUser(id, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identity', 'users'] });
      resetUserForm();
    },
    onError: (err: Error) => onError?.(err),
  });

  const deleteUserMutation = useMutation({
    mutationFn: (id: string) => deleteUser(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identity', 'users'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  // Role mutations
  const createRoleMutation = useMutation({
    mutationFn: (req: CreateRoleRequest) => createRole(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identity', 'roles'] });
      resetRoleForm();
    },
    onError: (err: Error) => onError?.(err),
  });

  const deleteRoleMutation = useMutation({
    mutationFn: (id: string) => deleteRole(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identity', 'roles'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const resetUserForm = () => {
    setShowUserForm(false);
    setEditingUser(null);
    setUserFormData({
      username: '',
      email: '',
      password: '',
      roles: [],
      enabled: true,
    });
  };

  const resetRoleForm = () => {
    setShowRoleForm(false);
    setRoleFormData({
      name: '',
      description: '',
      permissions: [],
    });
  };

  const handleEditUser = (user: User) => {
    setEditingUser(user);
    setUserFormData({
      username: user.username,
      email: user.email,
      password: '',
      roles: user.roles,
      enabled: user.enabled,
    });
    setShowUserForm(true);
  };

  const handleUserFormSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!userFormData.username.trim() || !userFormData.email.trim()) return;

    if (editingUser) {
      const req: UpdateUserRequest = {
        username: userFormData.username,
        email: userFormData.email,
        roles: userFormData.roles,
        enabled: userFormData.enabled,
      };
      if (userFormData.password) {
        req.password = userFormData.password;
      }
      updateUserMutation.mutate({ id: editingUser.id, req });
    } else {
      if (!userFormData.password) return;
      createUserMutation.mutate({
        username: userFormData.username,
        email: userFormData.email,
        password: userFormData.password,
        roles: userFormData.roles,
        enabled: userFormData.enabled,
      });
    }
  };

  const handleRoleFormSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!roleFormData.name.trim()) return;

    createRoleMutation.mutate({
      name: roleFormData.name,
      description: roleFormData.description,
      permissions: roleFormData.permissions,
    });
  };

  const toggleUserRole = (roleName: string) => {
    setUserFormData((prev) => ({
      ...prev,
      roles: prev.roles.includes(roleName)
        ? prev.roles.filter((r) => r !== roleName)
        : [...prev.roles, roleName],
    }));
  };

  const togglePermission = (permission: string) => {
    setRoleFormData((prev) => ({
      ...prev,
      permissions: prev.permissions.includes(permission)
        ? prev.permissions.filter((p) => p !== permission)
        : [...prev.permissions, permission],
    }));
  };

  const isLoading = usersQuery.isLoading || rolesQuery.isLoading;
  const error = usersQuery.error || rolesQuery.error;

  if (isLoading) {
    return (
      <div className="space-y-4 rounded-lg border p-4">
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
        </div>
        <div className="flex gap-2">
          <Skeleton className="h-10 w-24" />
          <Skeleton className="h-10 w-24" />
        </div>
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="flex items-center justify-between p-4 rounded-lg border">
              <div className="flex items-center gap-4">
                <Skeleton className="h-8 w-8 rounded" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-32" />
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Skeleton className="h-8 w-8" />
                <Skeleton className="h-8 w-8" />
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="card p-4">
        <div className="text-error">
          Error loading identity data. Identity service may not be configured.
        </div>
      </div>
    );
  }

  const users = usersQuery.data?.users || [];
  const roles = rolesQuery.data?.roles || [];
  const permissions = permissionsQuery.data?.permissions || [];

  return (
    <div className="card p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <Users className="h-5 w-5 text-primary" />
          <h2 className="text-lg font-semibold">Identity Management</h2>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              usersQuery.refetch();
              rolesQuery.refetch();
            }}
          >
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-2 border-b pb-2">
        <button
          onClick={() => setActiveTab('users')}
          className={cn(
            'px-4 py-2 rounded-t font-medium text-sm transition-colors',
            activeTab === 'users'
              ? 'bg-primary text-primary-foreground'
              : 'hover:bg-muted'
          )}
        >
          <Users className="h-4 w-4 inline-block mr-2" />
          Users ({users.length})
        </button>
        <button
          onClick={() => setActiveTab('roles')}
          className={cn(
            'px-4 py-2 rounded-t font-medium text-sm transition-colors',
            activeTab === 'roles'
              ? 'bg-primary text-primary-foreground'
              : 'hover:bg-muted'
          )}
        >
          <Shield className="h-4 w-4 inline-block mr-2" />
          Roles ({roles.length})
        </button>
      </div>

      {/* Users Tab */}
      {activeTab === 'users' && (
        <div className="space-y-4">
          <div className="flex justify-end">
            <Button
              size="sm"
              onClick={() => {
                resetUserForm();
                setShowUserForm(true);
              }}
            >
              <Plus className="h-4 w-4 mr-2" />
              New User
            </Button>
          </div>

          {/* User Form */}
          {showUserForm && (
            <div className="rounded-lg border p-4 bg-muted/50">
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-medium">
                  {editingUser ? 'Edit User' : 'Create New User'}
                </h3>
                <Button variant="ghost" size="sm" onClick={resetUserForm}>
                  <X className="h-4 w-4" />
                </Button>
              </div>
              <form onSubmit={handleUserFormSubmit} className="space-y-4">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium mb-1">Username *</label>
                    <input
                      type="text"
                      value={userFormData.username}
                      onChange={(e) =>
                        setUserFormData((prev) => ({ ...prev, username: e.target.value }))
                      }
                      placeholder="john.doe"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                      required
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1">Email *</label>
                    <input
                      type="email"
                      value={userFormData.email}
                      onChange={(e) =>
                        setUserFormData((prev) => ({ ...prev, email: e.target.value }))
                      }
                      placeholder="john.doe@example.com"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                      required
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-sm font-medium mb-1">
                    Password {editingUser ? '(leave empty to keep current)' : '*'}
                  </label>
                  <input
                    type="password"
                    value={userFormData.password}
                    onChange={(e) =>
                      setUserFormData((prev) => ({ ...prev, password: e.target.value }))
                    }
                    placeholder="Enter password"
                    className="w-full px-3 py-2 rounded-md border bg-background"
                    required={!editingUser}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2">Roles</label>
                  <div className="flex flex-wrap gap-2">
                    {roles.map((role) => (
                      <button
                        key={role.id}
                        type="button"
                        onClick={() => toggleUserRole(role.name)}
                        className={cn(
                          'px-3 py-1 rounded-full text-sm border transition-colors',
                          userFormData.roles.includes(role.name)
                            ? 'bg-primary text-primary-foreground border-primary'
                            : 'bg-background hover:bg-muted'
                        )}
                      >
                        {userFormData.roles.includes(role.name) && (
                          <Check className="h-3 w-3 inline-block mr-1" />
                        )}
                        {role.name}
                      </button>
                    ))}
                    {roles.length === 0 && (
                      <p className="text-sm text-muted-foreground">No roles available</p>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id="userEnabled"
                    checked={userFormData.enabled}
                    onChange={(e) =>
                      setUserFormData((prev) => ({ ...prev, enabled: e.target.checked }))
                    }
                    className="rounded border"
                  />
                  <label htmlFor="userEnabled" className="text-sm">
                    User enabled
                  </label>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    type="submit"
                    disabled={
                      createUserMutation.isPending ||
                      updateUserMutation.isPending ||
                      !userFormData.username.trim() ||
                      !userFormData.email.trim() ||
                      (!editingUser && !userFormData.password)
                    }
                  >
                    {createUserMutation.isPending || updateUserMutation.isPending ? (
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    ) : (
                      <Check className="h-4 w-4 mr-2" />
                    )}
                    {editingUser ? 'Update User' : 'Create User'}
                  </Button>
                  <Button type="button" variant="outline" onClick={resetUserForm}>
                    Cancel
                  </Button>
                </div>
                {(createUserMutation.error || updateUserMutation.error) && (
                  <p className="text-sm text-error">
                    {((createUserMutation.error || updateUserMutation.error) as Error).message}
                  </p>
                )}
              </form>
            </div>
          )}

          {/* User List */}
          {users.length === 0 ? (
            <div className="empty-state border rounded-lg">
              <Users className="empty-state-icon" />
              <p className="empty-state-title">No users found</p>
              <p className="empty-state-description">Click "New User" to create a user</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted">
                  <tr>
                    <th className="px-4 py-2 text-left font-medium">Status</th>
                    <th className="px-4 py-2 text-left font-medium">Username</th>
                    <th className="px-4 py-2 text-left font-medium">Email</th>
                    <th className="px-4 py-2 text-left font-medium">Roles</th>
                    <th className="px-4 py-2 text-left font-medium">Last Login</th>
                    <th className="px-4 py-2 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((user) => (
                    <tr key={user.id} className="border-t">
                      <td className="px-4 py-3">
                        <span
                          className={cn(
                            'inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium',
                            getUserStatusBadgeClass(user.enabled)
                          )}
                        >
                          {user.enabled ? (
                            <>
                              <UserCheck className="h-3 w-3" />
                              Active
                            </>
                          ) : (
                            <>
                              <UserX className="h-3 w-3" />
                              Disabled
                            </>
                          )}
                        </span>
                      </td>
                      <td className="px-4 py-3 font-medium">{user.username}</td>
                      <td className="px-4 py-3 text-muted-foreground">{user.email}</td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap gap-1">
                          {user.roles.length > 0 ? (
                            user.roles.map((role) => (
                              <span
                                key={role}
                                className="badge-info"
                              >
                                {role}
                              </span>
                            ))
                          ) : (
                            <span className="text-muted-foreground text-xs">No roles</span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        <span className="flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {formatLastLogin(user.last_login)}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleEditUser(user)}
                            title="Edit user"
                          >
                            <Pencil className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              if (confirm(`Delete user ${user.username}?`)) {
                                setActiveAction({ id: user.id, action: 'delete' });
                                deleteUserMutation.mutate(user.id);
                              }
                            }}
                            disabled={deleteUserMutation.isPending}
                            title="Delete user"
                          >
                            {activeAction?.id === user.id && activeAction.action === 'delete' ? (
                              <Loader2 className="h-4 w-4 animate-spin" />
                            ) : (
                              <Trash2 className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Roles Tab */}
      {activeTab === 'roles' && (
        <div className="space-y-4">
          <div className="flex justify-end">
            <Button
              size="sm"
              onClick={() => {
                resetRoleForm();
                setShowRoleForm(true);
              }}
            >
              <Plus className="h-4 w-4 mr-2" />
              New Role
            </Button>
          </div>

          {/* Role Form */}
          {showRoleForm && (
            <div className="rounded-lg border p-4 bg-muted/50">
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-medium">Create New Role</h3>
                <Button variant="ghost" size="sm" onClick={resetRoleForm}>
                  <X className="h-4 w-4" />
                </Button>
              </div>
              <form onSubmit={handleRoleFormSubmit} className="space-y-4">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium mb-1">Role Name *</label>
                    <input
                      type="text"
                      value={roleFormData.name}
                      onChange={(e) =>
                        setRoleFormData((prev) => ({ ...prev, name: e.target.value }))
                      }
                      placeholder="admin"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                      required
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1">Description</label>
                    <input
                      type="text"
                      value={roleFormData.description}
                      onChange={(e) =>
                        setRoleFormData((prev) => ({ ...prev, description: e.target.value }))
                      }
                      placeholder="Administrator role with full access"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2">Permissions</label>
                  <div className="flex flex-wrap gap-2 max-h-40 overflow-y-auto p-2 border rounded-md bg-background">
                    {permissions.length > 0 ? (
                      permissions.map((permission) => (
                        <button
                          key={permission}
                          type="button"
                          onClick={() => togglePermission(permission)}
                          className={cn(
                            'px-3 py-1 rounded-full text-xs border transition-colors',
                            roleFormData.permissions.includes(permission)
                              ? 'bg-primary text-primary-foreground border-primary'
                              : 'bg-background hover:bg-muted'
                          )}
                        >
                          {roleFormData.permissions.includes(permission) && (
                            <Check className="h-3 w-3 inline-block mr-1" />
                          )}
                          {permission}
                        </button>
                      ))
                    ) : (
                      <p className="text-sm text-muted-foreground">No permissions available</p>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    type="submit"
                    disabled={createRoleMutation.isPending || !roleFormData.name.trim()}
                  >
                    {createRoleMutation.isPending ? (
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    ) : (
                      <Plus className="h-4 w-4 mr-2" />
                    )}
                    Create Role
                  </Button>
                  <Button type="button" variant="outline" onClick={resetRoleForm}>
                    Cancel
                  </Button>
                </div>
                {createRoleMutation.error && (
                  <p className="text-sm text-error">
                    {(createRoleMutation.error as Error).message}
                  </p>
                )}
              </form>
            </div>
          )}

          {/* Role List */}
          {roles.length === 0 ? (
            <div className="empty-state border rounded-lg">
              <Shield className="empty-state-icon" />
              <p className="empty-state-title">No roles found</p>
              <p className="empty-state-description">Click "New Role" to create a role</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted">
                  <tr>
                    <th className="px-4 py-2 text-left font-medium">Name</th>
                    <th className="px-4 py-2 text-left font-medium">Description</th>
                    <th className="px-4 py-2 text-left font-medium">Permissions</th>
                    <th className="px-4 py-2 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {roles.map((role) => (
                    <tr key={role.id} className="border-t">
                      <td className="px-4 py-3">
                        <span className="flex items-center gap-2 font-medium">
                          <Shield className="h-4 w-4 text-primary" />
                          {role.name}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {role.description || '-'}
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap gap-1 max-w-md">
                          {role.permissions.length > 0 ? (
                            role.permissions.slice(0, 5).map((perm) => (
                              <span
                                key={perm}
                                className="badge-secondary"
                              >
                                {perm}
                              </span>
                            ))
                          ) : (
                            <span className="text-muted-foreground text-xs">No permissions</span>
                          )}
                          {role.permissions.length > 5 && (
                            <span className="badge-secondary">
                              +{role.permissions.length - 5} more
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            if (confirm(`Delete role ${role.name}?`)) {
                              setActiveAction({ id: role.id, action: 'delete' });
                              deleteRoleMutation.mutate(role.id);
                            }
                          }}
                          disabled={deleteRoleMutation.isPending}
                          title="Delete role"
                        >
                          {activeAction?.id === role.id && activeAction.action === 'delete' ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4" />
                          )}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
