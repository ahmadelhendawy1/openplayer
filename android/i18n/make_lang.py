# Builds assets/ui/lang_ar.js from the Windows player's Arabic strings plus the Android-only ones.
import json, re, sys
root = sys.argv[1]
s = open(root + "/assets/ui/index.html", encoding="utf-8").read()
win = json.load(open(sys.argv[2], encoding="utf-8"))
extra = json.load(open(root + "/i18n/android_ar.json", encoding="utf-8"))
keys = set(re.findall(r'data-t="([^"]+)"', s)) | set(re.findall(r'\bTf?\("((?:[^"\\]|\\.)+)"', s))
for m in re.findall(r'T\(\{([^}]*)\}\[', s): keys |= set(re.findall(r':\s*"([^"]+)"', m))
keys |= set(re.findall(r'\["([^"]+)", \[', s)) | set(re.findall(r'\["[a-z-]+", "([^"]+)", [01],', s))
keys |= set(extra)
out, missing = {}, []
for k in sorted(keys):
    v = extra.get(k, win.get(k))
    if v is None: missing.append(k)
    else: out[k] = v
open(root + "/assets/ui/lang_ar.js", "w", encoding="utf-8").write("window.LANG_AR = " + json.dumps(out, ensure_ascii=False, indent=0) + ";\n")
print(len(out), "strings; missing:", missing)
sys.exit(1 if missing else 0)
