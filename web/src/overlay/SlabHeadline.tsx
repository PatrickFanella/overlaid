import { Slab } from './Brand';
import { splitLastWord } from './data';

type SlabHeadlineProps = {
  text: string;
  as?: 'h1' | 'h2';
  className?: string;
};

// Poster headline with the last word set on the tilted green block.
export function SlabHeadline({ text, as: Tag = 'h2', className = '' }: SlabHeadlineProps) {
  const { lead, last } = splitLastWord(text);

  return (
    <Tag className={`sc-display sc-slab-host ${className}`}>
      {lead ? `${lead} ` : null}
      <Slab>{last}</Slab>
    </Tag>
  );
}
