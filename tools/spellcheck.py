#!/usr/bin/env python3
import re
import sys
from pathlib import Path

# Minimal built-in word list (small) + common technical words
COMMON = set("""
about above access account action active add admin all also and api are array as at auth
base bool build by cache call channel channels cli code config container data default
deploy device dir docker doc does done download env error event example exit file files
filter find fix for from fwd generator git go group handler host http https id if in info
json key lib list load local log logs make map method min mkdir name noop not of on one or
package path picon port post prod proxy queue read rebuild ref replace req res run
s3 save server service set shell show skip src ssl start status stream string struct
sure test text time to tmp tools try type url util value var view write xml yaml yes
""".split())

WORD_RE = re.compile(r"[A-Za-z][A-Za-z'-]{1,}")

# files to check
INCLUDE = ["README.md", "*.md", "*.yaml", "*.yml", "*.go", "*.txt"]

root = Path('.').resolve()

def load_system_dict():
    # try common locations
    paths = ["/usr/share/dict/words", "/usr/dict/words"]
    for p in paths:
        f = Path(p)
        if f.exists():
            return set(w.strip().lower() for w in f.read_text(encoding='utf-8', errors='ignore').splitlines() if w.strip())
    return None

sys_dict = load_system_dict()

if sys_dict is None:
    DICT = COMMON
else:
    DICT = sys_dict.union(COMMON)


def extract_text(path: Path) -> str:
    if path.suffix in ['.md', '.txt', '.yaml', '.yml']:
        return path.read_text(encoding='utf-8', errors='ignore')
    if path.suffix == '.go':
        s = path.read_text(encoding='utf-8', errors='ignore')
        # extract comments
        comments = []
        for line in s.splitlines():
            line = line.strip()
            if line.startswith('//'):
                comments.append(line[2:].strip())
            elif line.startswith('/*') or line.startswith('*'):
                comments.append(re.sub(r"/\*|\*/", '', line))
        return '\n'.join(comments)
    return ''


def words_from_text(text: str):
    for m in WORD_RE.finditer(text):
        yield m.group(0)


def main():
    files = []
    for p in root.rglob('*'):
        if p.is_file():
            if p.name.startswith('.git') or 'node_modules' in p.parts:
                continue
            if any(p.match(pat) for pat in ['README.md', '*.md', '*.go', '*.yaml', '*.yml', '*.txt']):
                files.append(p)

    issues = {}
    for f in files:
        text = extract_text(f)
        for w in words_from_text(text):
            lw = w.lower()
            if lw not in DICT and not lw.isnumeric() and len(lw) > 1 and lw not in COMMON:
                issues.setdefault(f, set()).add(w)

    if not issues:
        print('No obvious spelling issues found (heuristic).')
        return 0

    print('Potential spelling candidates (review manually):')
    for f, words in sorted(issues.items()):
        print(f"\n{f}:")
        for w in sorted(words):
            print('  ', w)
    return 0

if __name__ == '__main__':
    sys.exit(main())
