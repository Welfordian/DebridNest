import { useState } from 'react';
import { copyText } from '../lib/copy';
import Icon from './Icon';

export default function CopyButton({
  value,
  label = 'Copy',
  className = 'btn btn-secondary btn-sm',
}: {
  value: string;
  label?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    const ok = await copyText(value);
    if (ok) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    }
  }

  return (
    <button type="button" className={className} onClick={handleCopy} disabled={!value}>
      <Icon
        name={copied ? 'check' : 'copy'}
        size={14}
        style={copied ? { color: 'var(--success)' } : undefined}
      />
      {copied ? 'Copied' : label}
    </button>
  );
}
