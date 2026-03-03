import { describe, it, expect } from 'vitest';
import { normalizeEpgText } from './text';

describe('normalizeEpgText', () => {
  it('handles empty input', () => {
    expect(normalizeEpgText('')).toBe('');
    expect(normalizeEpgText(undefined)).toBe('');
  });

  it('unescapes literal newlines', () => {
    expect(normalizeEpgText('Hello\\nWorld')).toBe('Hello\nWorld');
    expect(normalizeEpgText('Line 1\\r\\nLine 2')).toBe('Line 1\nLine 2');
  });

  it('decodes common named entities', () => {
    expect(normalizeEpgText('&quot;Quoted&quot;')).toBe('"Quoted"');
    expect(normalizeEpgText('Bread &amp; Butter')).toBe('Bread & Butter');
    expect(normalizeEpgText('&lt;div&gt;')).toBe('<div>');
    expect(normalizeEpgText('User&apos;s guide')).toBe("User's guide");
    expect(normalizeEpgText('Non&nbsp;breaking')).toBe('Non breaking');
  });

  it('decodes decimal entities', () => {
    expect(normalizeEpgText('&#39;Single quote&#39;')).toBe("'Single quote'");
    expect(normalizeEpgText('&#65;')).toBe('A');
  });

  it('decodes hex entities', () => {
    expect(normalizeEpgText('&#x27;Single quote&#x27;')).toBe("'Single quote'");
    expect(normalizeEpgText('&#x41;')).toBe('A');
    expect(normalizeEpgText('&#X41;')).toBe('A');
  });

  it('is idempotent for already decoded text', () => {
    const text = 'Normal text with "quotes" and & symbols.';
    expect(normalizeEpgText(text)).toBe(text);
  });

  it('preserves unknown entities', () => {
    expect(normalizeEpgText('&unknown;')).toBe('&unknown;');
  });

  it('handles mixed content', () => {
    const input = 'Show: &quot;The Loop&quot;\\nDescription: &apos;Interesting&#39; &amp; fun!';
    const expected = "Show: \"The Loop\"\nDescription: 'Interesting' & fun!";
    expect(normalizeEpgText(input)).toBe(expected);
  });
});
