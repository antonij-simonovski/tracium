/**
 * Pretty-print any JSON objects or arrays found in the text with 2-space
 * indentation, in place. JSON may span the whole string or be embedded in
 * surrounding prose; non-JSON content passes through unchanged. Scanning stops
 * at the first bracket that never closes, keeping the work linear.
 */
export function prettifyMaybeJson(text: string): string {
  let out = '';
  let last = 0;
  for (let i = 0; i < text.length; i++) {
    if (text[i] !== '{' && text[i] !== '[') continue;
    const end = findBalancedEnd(text, i);
    if (end === -1) break;
    out += text.slice(last, i) + prettifyGroup(text.slice(i, end + 1));
    last = end + 1;
    i = end;
  }
  return out + text.slice(last);
}

function prettifyGroup(group: string): string {
  try {
    const value = JSON.parse(group);
    // A one-element scalar array is more likely prose, like a "[1]" citation.
    const isScalarRef = Array.isArray(value) && value.length === 1 && (value[0] === null || typeof value[0] !== 'object');
    return isScalarRef ? group : JSON.stringify(value, null, 2);
  } catch {
    return group;
  }
}

/**
 * Index of the bracket closing the one at `start`, respecting JSON string
 * literals and escapes, or -1 if the text ends before it balances.
 */
function findBalancedEnd(text: string, start: number): number {
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let i = start; i < text.length; i++) {
    const ch = text[i];
    if (inString) {
      if (escaped) escaped = false;
      else if (ch === '\\') escaped = true;
      else if (ch === '"') inString = false;
    } else if (ch === '"') {
      inString = true;
    } else if (ch === '{' || ch === '[') {
      depth++;
    } else if (ch === '}' || ch === ']') {
      depth--;
      if (depth === 0) return i;
    }
  }
  return -1;
}
