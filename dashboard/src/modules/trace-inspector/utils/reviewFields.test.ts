import { describe, expect, it } from 'vitest';
import { applyEdits, flattenFields, parseJsonObject } from './reviewFields';

describe('reviewFields', () => {
  it('parses fenced JSON objects only', () => {
    expect(parseJsonObject('```json\n{"a": 1}\n```')).toEqual({ a: 1 });
    expect(parseJsonObject('assistant: {"a": 1}')).toEqual({ a: 1 });
    expect(parseJsonObject('[1]')).toBeNull();
    expect(parseJsonObject('hello')).toBeNull();
    expect(parseJsonObject(null)).toBeNull();
  });

  it('flattens nested objects and keeps arrays whole', () => {
    const fields = flattenFields({ total: 10, customer: { city: 'Oslo', tags: ['a'] }, meta: {} });
    expect(fields.map(f => f.path)).toEqual(['total', 'customer.city', 'customer.tags', 'meta']);
  });

  it('applies edits keeping string fields as strings', () => {
    const obj = { total: 10, zip: '0150', 'a.b': { c: true } };
    const fields = flattenFields(obj);
    const out = applyEdits(obj, fields, { total: '12.5', zip: '5003', 'a.b.c': 'false' });
    expect(out).toEqual({ total: 12.5, zip: '5003', 'a.b': { c: false } });
    expect(obj.total).toBe(10);
  });
});
