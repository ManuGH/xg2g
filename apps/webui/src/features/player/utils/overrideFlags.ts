export function isTruthyOverrideValue(value: string | null | undefined): boolean {
  if (typeof value !== 'string') return false;
  switch (value.trim().toLowerCase()) {
    case '1':
    case 'true':
    case 'on':
    case 'yes':
    case 'enabled':
      return true;
    default:
      return false;
  }
}
