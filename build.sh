#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
GO=${GO:-go}
# Generate resources first so native Windows test binaries also embed the manifest.
"$GO" run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -ico assets/robocopy-studio.ico -arch amd64 -o cmd/robocopygui/rsrc_windows_amd64.syso
"$GO" test -v -cover ./...
"$GO" vet ./...
mkdir -p bin
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$GO" build -buildvcs=false -trimpath -ldflags='-H windowsgui -s -w' -o bin/RoboCopyGUI.exe ./cmd/robocopygui
GOOS=windows GOARCH=amd64 "$GO" vet ./...
python3 tools/verify_pe.py bin/RoboCopyGUI.exe
python3 - <<'PY'
from pathlib import Path
import hashlib
import zipfile
files = [Path("bin/RoboCopyGUI.exe"), Path("README.md"), Path("SECURITY.md"), Path("LICENSE")]
files += sorted(Path("assets").glob("robocopy-studio.*"))
files += sorted(Path("docs").rglob("*.md"))
files += sorted(Path("docs/screenshots").glob("*.png"))
files += sorted(Path("notices").glob("*.txt"))
notice = Path("THIRD_PARTY_NOTICES.md")
if notice.exists():
    files.append(notice)
archive = Path("bin/RoboCopyStudio-win64.zip")
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
    for p in files:
        z.write(p, "RoboCopyGUI.exe" if p.name == "RoboCopyGUI.exe" else p.as_posix())
with zipfile.ZipFile(archive) as z:
    assert z.testzip() is None
    for p in files:
        name = "RoboCopyGUI.exe" if p.name == "RoboCopyGUI.exe" else p.as_posix()
        assert z.read(name) == p.read_bytes(), name
checksums = "".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in (Path("bin/RoboCopyGUI.exe"), archive))
Path("bin/SHA256SUMS").write_text(checksums, encoding="ascii")
print(checksums, end="")
PY
