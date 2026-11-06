type JsonObject = Record<string, unknown>;

export interface FlatField {
  path: string;
  keys: string[];
  value: unknown;
}

/** Parses a JSON object, allowing an LLM span's "assistant:" prefix and a ```json fence; null for anything else. */
export function parseJsonObject(text: string | null | undefined): JsonObject | null {
  if (!text) return null;
  const body = text.trim().replace(/^assistant:\s*/, '').replace(/^```(?:json)?\s*/, '').replace(/\s*```$/, '');
  try {
    const v: unknown = JSON.parse(body);
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as JsonObject) : null;
  } catch {
    return null;
  }
}

/** Flattens nested objects to leaf fields the way the API grades them; arrays stay whole. */
export function flattenFields(obj: JsonObject, keys: string[] = []): FlatField[] {
  const entries = Object.entries(obj);
  if (entries.length === 0 && keys.length > 0) return [{ path: keys.join('.'), keys, value: obj }];
  return entries.flatMap(([k, v]) => {
    const next = [...keys, k];
    return v && typeof v === 'object' && !Array.isArray(v)
      ? flattenFields(v as JsonObject, next)
      : [{ path: next.join('.'), keys: next, value: v }];
  });
}

/** Display text for a field value: strings bare, everything else as JSON. */
export function fieldText(value: unknown): string {
  return typeof value === 'string' ? value : JSON.stringify(value);
}

/** Reads edited text back: a string field stays a string, others parse as JSON when they can. */
export function parseFieldText(text: string, original: unknown): unknown {
  if (typeof original === 'string') return text;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

/** Rebuilds the object with edited leaf values, leaving the original untouched. */
export function applyEdits(obj: JsonObject, fields: FlatField[], edits: Record<string, string>): JsonObject {
  const out = structuredClone(obj);
  for (const f of fields) {
    if (!(f.path in edits)) continue;
    let node = out;
    for (const k of f.keys.slice(0, -1)) node = node[k] as JsonObject;
    node[f.keys[f.keys.length - 1]] = parseFieldText(edits[f.path], f.value);
  }
  return out;
}
