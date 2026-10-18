import { describe, it, expect } from 'vitest';
import { prettifyMaybeJson } from './prettifyMaybeJson';

describe('prettifyMaybeJson', () => {
  it('prettifies a string that is entirely JSON', () => {
    expect(prettifyMaybeJson('{"a":1,"b":[2,3]}')).toBe(
      '{\n  "a": 1,\n  "b": [\n    2,\n    3\n  ]\n}',
    );
  });

  it('prettifies JSON embedded in surrounding text', () => {
    expect(prettifyMaybeJson('Tool returned {"ok":true} after retry')).toBe(
      'Tool returned {\n  "ok": true\n} after retry',
    );
  });

  it('prettifies multiple embedded JSON segments independently', () => {
    expect(prettifyMaybeJson('in: [1,2] out: {"x":0}')).toBe(
      'in: [\n  1,\n  2\n] out: {\n  "x": 0\n}',
    );
  });

  it('leaves plain text and invalid JSON untouched', () => {
    expect(prettifyMaybeJson('hello world')).toBe('hello world');
    expect(prettifyMaybeJson('set {a: 1} here')).toBe('set {a: 1} here');
    expect(prettifyMaybeJson('unbalanced {"a": [1,2')).toBe('unbalanced {"a": [1,2');
  });

  it('does not treat brackets inside JSON strings as structure', () => {
    expect(prettifyMaybeJson('{"msg":"brace } and quote \\" inside"}')).toBe(
      '{\n  "msg": "brace } and quote \\" inside"\n}',
    );
  });

  it('leaves single-scalar brackets in prose alone', () => {
    expect(prettifyMaybeJson('see [1] and ["a"]')).toBe('see [1] and ["a"]');
  });

  it('handles deeply unbalanced and invalid nesting in linear time', () => {
    const open = '['.repeat(200_000);
    expect(prettifyMaybeJson(open)).toBe(open);
    const invalid = '[x'.repeat(100_000) + ']'.repeat(100_000);
    expect(prettifyMaybeJson(invalid)).toBe(invalid);
  });
});
