# Release validation

## Current update: compact administrator action

Production EXE SHA-256: `9346d81bdd3f2e4cb50da26dbd26457cd274918c365ee3f1b0fb27dd2acdbec0`.

The optional administrator-restart action is now width-bounded and anchored to the right, using the same dark secondary-button styling and toolbar height. The permissions explanation has enough width to remain on one line at supported window widths. Native Windows layout checks verified its fixed 240-DIP width, matching toolbar height and right alignment at default, maximized, minimum-width and restored sizes. Actual screenshots were inspected. Non-elevated startup, safe refusal of the administrator confirmation, a hash-checked one-off copy, plain-language summary, calendar opening, clean exit and acceptance-task cleanup passed again. No privilege, copy, scheduling or signing behavior changed.

## Previous update: plain-language Summary table

Production EXE SHA-256: `1e8762f9dbf4f20b74870c19f3bf713b199b001c9720a7e7a1cac9e52245395e`.

This update changes explanatory wording only, not copying, error detection or permissions. Portable race tests, build/vet and PE checks passed. The Windows `TestSummaryPlainLanguage` regression verified the empty state and verbatim known-error details. A real non-elevated GUI copy completed with matching file hashes; its live Summary text was read back and checked for the new explanations and removal of the old jargon. Default one-off behavior, calendar opening, optional-elevation refusal and clean exit passed again. The main-window screenshot was refreshed from that run. The detailed normal-user/scheduling validation below refers to the preceding baseline release.

## Baseline: normal-user startup

Base release EXE SHA-256:

```text
f03f21b7a0ef414c7a38f15bc0d59a55c79ee779133139308672f80e48696e2d
```

## Observed results

- Portable race tests, vet, Windows cross-build and actual PE-resource checks passed. The production binary is Windows GUI/AMD64, with Common Controls v6, embedded icon and `asInvoker` rather than mandatory elevation.
- A genuine temporary local account with no Administrators membership ran the native privilege/dialog/date-picker/DPAPI/worker tests and the actual production EXE smoke. Its process reported **not elevated**. Unicode/empty/nested copies, read-only preview, fixture-only mirror deletion, filters, MT activity, profile/history/audit persistence and locked-file failure evidence passed.
- Future one-time, daily and weekly registrations read back with effective `LeastPrivilege` and were deleted successfully as the genuine standard user. The same native registration suite passed from an elevated account, still requesting least privilege.
- Windows can omit the optional default `RunLevel` element when exporting least-privilege task XML. This was reproduced natively and fixed with regression coverage: absence means the service's default `LeastPrivilege`; an explicitly empty or arbitrary value remains rejected. Exact SID/action/logon ownership checks remain enforced.
- The existing interactive desktop launched the production GUI with an independently checked **non-elevated token**. The explicit administrator confirmation was opened and declined; the same normal process stayed open. The default unticked schedule action copied fixture content with matching hashes and created no task. The mini calendar opened and clean application exit was verified.
- Windows Task Scheduler launched the exact production EXE through an owned `InteractiveToken`/`LeastPrivilege` task. Copied hashes matched, preflight/copy history was complete and scheduler result was 0. The task was removed afterward.
- Temporary acceptance accounts, profiles and successful harness tasks were removed and absence verified. Failed fixture registrations encountered while fixing omitted run-level handling were also explicitly identified and removed, without touching user schedules.
- README screenshots are actual captures from the tested normal-token Windows GUI, not mockups. Transparent resize-border pixels can be trimmed to exclude unrelated desktop content; displayed controls/results are not synthesized.

## Scope and retained private evidence

Tests ran on the authorized Windows 10 Pro build 18363 VM. Detailed fixture reports and native transcripts remain in the development workspace's ignored `bin/` directory; they are not included in the public Git source handoff. The release ZIP contains the executable, public documentation, screenshots, artwork and third-party notices.

WinRM service-desktop secondary logon was unsuitable for the fresh-account GUI libraries; the successful genuine-account test used the existing interactive desktop with a one-use, current-user-DPAPI-protected test credential, deleted after loading. The product does not use that harness or store account passwords.

This is not proof of every Windows version, enterprise policy, locale/DPI, network-share/alternate-account scenario, sleep/logoff or multi-day/DST recurrence. Secure-desktop UAC approval was not automatically accepted: transition cancellation/failure is unit-tested and safe refusal was tested in the real UI. The broader security audit remains unfinished. The EXE is unsigned.
