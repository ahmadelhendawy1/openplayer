#!/usr/bin/env bash
# Release gate: refuse to publish an engine whose binaries were configured
# with GPL-only or non-free options. Usage: license-gate.sh DIR
set -euo pipefail
dir="${1:?usage: license-gate.sh DIR}"
fail=0
shopt -s nullglob
dlls=("$dir"/*.dll)
if [ ${#dlls[@]} -eq 0 ]; then echo "no DLLs found in $dir"; exit 1; fi
for f in "${dlls[@]}"; do
  if grep -aqE -- '--enable-(gpl|nonfree|version3)|--enable-lib(x264|x265|xvid|fdk-aac)' "$f"; then
    echo "REFUSED: $(basename "$f") was built with GPL/non-free options"; fail=1
  fi
done
[ $fail -eq 0 ] && echo "license gate passed (${#dlls[@]} DLLs checked)"
exit $fail
