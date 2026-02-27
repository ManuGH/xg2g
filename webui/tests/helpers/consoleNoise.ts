type Matcher = string | RegExp;

type ConsoleNoiseOptions = {
  error?: Matcher[];
  warn?: Matcher[];
};

function textFromArgs(args: unknown[]): string {
  return args
    .map((arg) => {
      if (typeof arg === 'string') return arg;
      if (arg instanceof Error) return `${arg.name}: ${arg.message}`;
      try {
        return JSON.stringify(arg);
      } catch {
        return String(arg);
      }
    })
    .join(' ');
}

function matchesAny(text: string, matchers: Matcher[]): boolean {
  return matchers.some((matcher) =>
    typeof matcher === 'string' ? text.includes(matcher) : matcher.test(text),
  );
}

export function suppressExpectedConsoleNoise(options: ConsoleNoiseOptions): () => void {
  const errorMatchers = options.error ?? [];
  const warnMatchers = options.warn ?? [];

  const originalError = console.error.bind(console);
  const originalWarn = console.warn.bind(console);

  console.error = ((...args: unknown[]) => {
    const text = textFromArgs(args);
    if (matchesAny(text, errorMatchers)) return;
    originalError(...args);
  }) as typeof console.error;

  console.warn = ((...args: unknown[]) => {
    const text = textFromArgs(args);
    if (matchesAny(text, warnMatchers)) return;
    originalWarn(...args);
  }) as typeof console.warn;

  return () => {
    console.error = originalError;
    console.warn = originalWarn;
  };
}
