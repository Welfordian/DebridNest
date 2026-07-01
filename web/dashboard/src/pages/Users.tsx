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
import Icon from '../components/Icon';
import Modal from '../components/Modal';
import { TopBarActions, TopBarMeta } from '../components/TopBar';
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
      <p className="section-desc">
        Copy the token for <strong>{userName}</strong> now — it will not be shown again.
      </p>
      <code className="token-display">{token}</code>
      <div className="modal-actions">
        <CopyButton value={token} label="Copy token" className="btn btn-secondary" />
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
      const result = await createUser({ name: trimmed, role: admin ? 'admin' : 'user' });
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
        <label className="toggle-label" style={{ marginTop: 14 }}>
          <input type="checkbox" checked={admin} onChange={(e) => setAdmin(e.target.checked)} />
          <span>Admin access</span>
        </label>
        <div className="modal-actions">
          <button type="button" className="btn btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy || !name.trim()}>
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
    <div className="page">
      <TopBarMeta>
        {list.length} user{list.length === 1 ? '' : 's'}
      </TopBarMeta>
      <TopBarActions>
        <button type="button" className="btn btn-primary btn-sm" onClick={() => setShowCreate(true)}>
          <Icon name="plus" size={14} />
          Create user
        </button>
      </TopBarActions>

      {list.length === 0 ? (
        <div className="empty-state card">
          <div className="empty-state-icon">
            <Icon name="users" size={20} />
          </div>
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
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {list.map((user) => (
                <tr key={user.id}>
                  <td>
                    <span className="name-primary">{user.name}</span>
                    {user.role === 'admin' && (
                      <span className="pill pill-accent user-admin-pill">Admin</span>
                    )}
                  </td>
                  <td className="muted">{user.role}</td>
                  <td className="muted">
                    {user.createdAt ? formatRelativeTime(user.createdAt) : '—'}
                  </td>
                  <td className="actions-cell">
                    <span className="row-actions">
                      <button
                        type="button"
                        className="icon-btn"
                        title="Rotate token"
                        aria-label={`Rotate token for ${user.name}`}
                        disabled={busyId === user.id}
                        onClick={() => handleRotate(user)}
                      >
                        <Icon name="key" size={18} />
                      </button>
                      <button
                        type="button"
                        className="icon-btn"
                        title="Delete user"
                        aria-label={`Delete ${user.name}`}
                        disabled={busyId === user.id}
                        onClick={() => handleDelete(user)}
                      >
                        <Icon name="trash-2" size={18} />
                      </button>
                    </span>
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
