import { useState } from 'react';
import { copyText } from '../lib/copy';

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
      window.setTimeout(() => setCopied(false), 1500);
    }
  }

  return (
    <button type="button" className={`copy-btn ${className}`.trim()} onClick={handleCopy} disabled={!value}>
      {copied ? 'Copied!' : label}
    </button>
  );
}
