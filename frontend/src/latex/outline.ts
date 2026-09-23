// Document structure, parsed from the buffer on screen.
//
// This deliberately reads the live buffer rather than asking the backend, which
// reads what is on disk. An outline that only updates when you save is an
// outline that disagrees with the document in front of you — and the moment you
// notice it lying, you stop trusting it.

export interface OutlineItem {
  /** 0 for \part down to 5 for \paragraph, so nesting is a number comparison. */
  level: number;
  title: string;
  /** 1-based, for goToLine. */
  line: number;
}

const LEVELS: Record<string, number> = {
  part: 0,
  chapter: 1,
  section: 2,
  subsection: 3,
  subsubsection: 4,
  paragraph: 5,
};

const HEADING_RE = /\\(part|chapter|section|subsection|subsubsection|paragraph)\*?\s*\{/;

export function outline(text: string): OutlineItem[] {
  const items: OutlineItem[] = [];

  text.split('\n').forEach((raw, index) => {
    const line = stripComment(raw);
    const match = HEADING_RE.exec(line);
    if (!match) return;

    const title = braceGroup(line, match.index + match[0].length - 1);
    if (title === null) return;

    items.push({
      level: LEVELS[match[1]],
      title: flatten(title) || '(untitled)',
      line: index + 1,
    });
  });

  return items;
}

/** Reads the balanced {...} starting at open, so a title may contain braces. */
function braceGroup(s: string, open: number): string | null {
  if (s[open] !== '{') return null;
  let depth = 0;
  for (let i = open; i < s.length; i++) {
    if (s[i] === '{') depth++;
    else if (s[i] === '}' && --depth === 0) return s.slice(open + 1, i);
  }
  // An unclosed brace means the heading is still being typed; take the rest of
  // the line so the entry appears as you write it rather than popping in later.
  return s.slice(open + 1);
}

/** Reduces a heading to something that reads as one line in a list. */
function flatten(s: string): string {
  return s
    .replace(/\\[a-zA-Z]+\s*/g, '')
    .replace(/[{}~]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

function stripComment(line: string): string {
  for (let i = 0; i < line.length; i++) {
    if (line[i] !== '%') continue;
    let slashes = 0;
    for (let j = i - 1; j >= 0 && line[j] === '\\'; j--) slashes++;
    if (slashes % 2 === 0) return line.slice(0, i);
  }
  return line;
}
