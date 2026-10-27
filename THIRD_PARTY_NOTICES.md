# Third-party notices

RoboCopy Studio uses the following components. Their unmodified license texts are included under `notices/` and accompany the release ZIP.

| Component | Version used | Notice |
|---|---|---|
| Go runtime / standard library | Toolchain selected by `go.mod` | [Go license](notices/go.LICENSE.txt) |
| github.com/lxn/walk | v0.0.0-20210112085537-c389da54e794 | [Walk license](notices/walk.LICENSE.txt) |
| github.com/lxn/win | v0.0.0-20210218163916-a377121e959e | [Win license](notices/win.LICENSE.txt) |
| golang.org/x/sys | v0.49.0 | [x/sys license](notices/x-sys.LICENSE.txt) |
| gopkg.in/Knetic/govaluate.v3 | v3.0.0 | [Govaluate license](notices/govaluate.LICENSE.txt) |

The pinned `github.com/akavel/rsrc` resource generator is development tooling, not an end-user runtime dependency. Windows Robocopy and system components are supplied by Windows, not bundled as separate programs.

First-party RoboCopy Studio source, documentation and original artwork are licensed under the [MIT License](LICENSE). Third-party components retain their own license terms reproduced under `notices/`; the project's MIT license does not replace those terms or license Windows components.
