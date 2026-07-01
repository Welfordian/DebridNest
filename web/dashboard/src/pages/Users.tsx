import { FormEvent, useCallback, useState } from 'react';
import {
  createUser,
  deleteUser,
  fetchUsers,
  rotateUserToken,
  type CreateUserResponse,
  type DashboardUser,
} from '../api';
import CopyButton from '../components/CopyButton';
import Modal from '../components/Modal';
import { useToast } from '../components/Toast';
import { usePolling } from '../hooks/usePolling';
import { formatRelativeTime } from '../lib/format';

function TokenModal({
  title,
  userName,
  token,
  onClose,
}: {
  title: string;
  userName: string;
  token: string;
  onClose: () => void;
}) {
  return (
    <Modal title={title} onClose={onClose}>
      <p className="muted section-desc">
        Copy the token for <strong>{userName}</strong> now — it will not be shown again.
      </p>
      <code className="token-display">{token}</code>
      <div className="modal-actions">
        <CopyButton value={token} label="Copy token" />
        <button type="button" className="btn btn-primary" onClick={onClose}>
          Done
        </button>
      </div>
    </Modal>
  );
}

function CreateUserModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (result: CreateUserResponse) => void;
}) {
  const { toast } = useToast();
  const [name, setName] = useState('');
  const [role, setRole] = useState('user');
  const [admin, setAdmin] = useState(false);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      toast('Name is required', 'error');
      return;
    }

    setBusy(true);
    try {
      const result = await createUser({ name: trimmed, role: admin ? 'admin' : role || 'user' });
      onCreated(result);
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Failed to create user', 'error');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Create user" onClose={onClose}>
      <form onSubmit={handleSubmit}>
        <div className="form-group">
          <label htmlFor="user-name">Name</label>
          <input
            id="user-name"
            className="input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. media-server"
            autoFocus
          />
        </div>
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="user-role">Role</label>
            <input
              id="user-role"
              className="input"
              value={role}
              onChange={(e) => setRole(e.target.value)}
              placeholder="user"
            />
          </div>
          <div className="form-group toggle-group">
            <label className="toggle-label">
              <input
                type="checkbox"
                checked={admin}
                onChange={(e) => setAdmin(e.target.checked)}
              />
              <span>Admin access</span>
            </label>
          </div>
        </div>
        <div className="modal-actions">
          <button type="button" className="btn btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? 'Creating…' : 'Create user'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

export default function Users() {
  const { toast } = useToast();
  const loader = useCallback(() => fetchUsers(), []);
  const { data: users, error, loading, refresh } = usePolling(loader, { intervalMs: 15000 });

  const [showCreate, setShowCreate] = useState(false);
  const [tokenModal, setTokenModal] = useState<{ title: string; userName: string; token: string } | null>(
    null,
  );
  const [busyId, setBusyId] = useState<string | null>(null);

  async function handleDelete(user: DashboardUser) {
    if (!confirm(`Delete user "${user.name}"? Their token will stop working immediately.`)) return;

    setBusyId(user.id);
    try {
      await deleteUser(user.id);
      toast(`Deleted ${user.name}`);
      await refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Delete failed', 'error');
    } finally {
      setBusyId(null);
    }
  }

  async function handleRotate(user: DashboardUser) {
    if (!confirm(`Rotate token for "${user.name}"? The old token will stop working.`)) return;

    setBusyId(user.id);
    try {
      const result = await rotateUserToken(user.id);
      setTokenModal({
        title: 'Token rotated',
        userName: user.name,
        token: result.token,
      });
      await refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Rotate failed', 'error');
    } finally {
      setBusyId(null);
    }
  }

  if (loading && !users) {
    return <p className="muted page-loading">Loading users…</p>;
  }

  if (error && !users) {
    return (
      <div className="page-error card">
        <p className="error">{error}</p>
        <button type="button" className="btn btn-secondary" onClick={() => refresh()}>
          Retry
        </button>
      </div>
    );
  }

  const list = users ?? [];

  return (
    <div className="users-page">
      <div className="page-toolbar">
        <p className="toolbar-meta muted">{list.length} user{list.length === 1 ? '' : 's'}</p>
        <button type="button" className="btn btn-primary" onClick={() => setShowCreate(true)}>
          Add user
        </button>
      </div>

      {list.length === 0 ? (
        <div className="empty-state card">
          <p>No users yet.</p>
          <p className="muted">Create a user to issue API tokens.</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Role</th>
                <th>Created</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {list.map((user) => (
                <tr key={user.id}>
                  <td>
                    <span className="name-primary">{user.name}</span>
                    {user.role === 'admin' && (
                      <span className="pill pill-live user-admin-pill">admin</span>
                    )}
                  </td>
                  <td>{user.role}</td>
                  <td className="muted">
                    {user.createdAt ? formatRelativeTime(user.createdAt) : '—'}
                  </td>
                  <td>
                    <div className="actions-cell">
                      <button
                        type="button"
                        className="btn btn-secondary btn-sm"
                        disabled={busyId === user.id}
                        onClick={() => handleRotate(user)}
                      >
                        Rotate token
                      </button>
                      <button
                        type="button"
                        className="btn btn-danger btn-sm"
                        disabled={busyId === user.id}
                        onClick={() => handleDelete(user)}
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {error && <p className="error">{error}</p>}

      {showCreate && (
        <CreateUserModal
          onClose={() => setShowCreate(false)}
          onCreated={(result) => {
            setShowCreate(false);
            setTokenModal({
              title: 'User created',
              userName: result.name,
              token: result.token,
            });
            refresh();
          }}
        />
      )}

      {tokenModal && (
        <TokenModal
          title={tokenModal.title}
          userName={tokenModal.userName}
          token={tokenModal.token}
          onClose={() => setTokenModal(null)}
        />
      )}
    </div>
  );
}
