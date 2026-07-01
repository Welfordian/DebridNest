import type { FormEvent, RefObject } from 'react';
import Icon from '../../components/Icon';

interface AddTorrentPanelProps {
  magnet: string;
  busy: boolean;
  error: string | null;
  fileInputRef: RefObject<HTMLInputElement>;
  onMagnetChange: (value: string) => void;
  onSubmit: (event: FormEvent) => void;
  onUpload: (file: File) => void;
}

export default function AddTorrentPanel({
  magnet,
  busy,
  error,
  fileInputRef,
  onMagnetChange,
  onSubmit,
  onUpload,
}: AddTorrentPanelProps) {
  return (
    <form className="add-torrent-bar card" onSubmit={onSubmit}>
      <div className="card-heading">
        <h2>Add torrent</h2>
      </div>
      <div className="add-torrent-row">
        <textarea
          id="magnet-input"
          className="magnet-input"
          value={magnet}
          onChange={(event) => onMagnetChange(event.target.value)}
          placeholder="magnet:?xt=urn:btih:..."
          rows={2}
        />
        <div className="add-torrent-actions">
          <button type="submit" className="btn btn-primary" disabled={busy || !magnet.trim()}>
            <Icon name="magnet" size={14} />
            {busy ? 'Adding...' : 'Add magnet'}
          </button>
          <label className="btn btn-secondary upload-label">
            <Icon name="upload" size={14} />
            Upload .torrent
            <input
              ref={fileInputRef}
              type="file"
              accept=".torrent,application/x-bittorrent"
              hidden
              onChange={(event) => {
                const file = event.target.files?.[0];
                if (file) onUpload(file);
              }}
            />
          </label>
        </div>
      </div>
      {error && <p className="error">{error}</p>}
    </form>
  );
}
