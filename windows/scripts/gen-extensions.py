#!/usr/bin/env python3
"""Generate NSIS include files from cmd/openplayer/extensions.txt (single source of truth)."""
import sys, pathlib
src = pathlib.Path(sys.argv[1]); out = pathlib.Path(sys.argv[2])
exts = []
for line in src.read_text().splitlines():
    kind, _, rest = line.partition(':')
    if kind.strip() in ('video', 'audio'):
        exts += rest.split()
exts = sorted(set(exts))
def write(name, macro):
    (out / name).write_text(''.join(f'  !insertmacro {macro} {e}\n' for e in exts))
write('ext-install.nsh', 'RegExt')
write('ext-uninstall.nsh', 'UnRegExt')
write('ext-context.nsh', 'CtxExt')
write('ext-context-uninstall.nsh', 'UnCtxExt')
print(f'{len(exts)} extensions')
