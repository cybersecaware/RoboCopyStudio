# Build and native validation

## Portable / cross-target checks

From the repository root on the Linux development host:

```bash
bash build.sh
go test -race ./internal/...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o bin/native-tests.exe ./cmd/robocopygui
python3 tools/verify_pe.py bin/RoboCopyGUI.exe
```

`build.sh` generates `rsrc_windows_amd64.syso` before tests/build so Windows binaries receive the `asInvoker`/Common Controls manifest and application icon. Generated resources and binaries are ignored by Git. PE inspection must report AMD64, Windows GUI, manifest ID 1 with `asInvoker`, Common Controls v6 and the expected multi-resolution icon.

Portable tests cover command/quoting and option validation, bounds, storage behavior, scheduling/date/DST validation, native task-XML ownership rules and picker-selection conversion. Native privilege tests cover token querying, advice, decline/cancellation/failure/success transition decisions. Simulated launch callbacks are not proof of an actual secure-desktop UAC interaction.

## Windows smoke (disposable files only)

Run from a writable folder as an ordinary user; do not choose Run as administrator for this check:

```powershell
$p = Start-Process .\RoboCopyGUI.exe -ArgumentList '--smoke-test smoke.json' -PassThru
$null = $p.Handle
$p.WaitForExit()
Get-Content .\smoke.json -Raw
```

Require `ok: true`, `elevated: false` and no `privilege_error`. The production EXE uses the same runner/log/parser path as the GUI with owned temporary fixtures, not fabricated engine results. It verifies Unicode/empty/nested content, read-only preview, fixture-scoped mirror deletion, filter/MT behavior, actual native summary/progress, process-I/O samples, profile/history round trips and a real locked-file failure. Fixtures are scoped and removed by the smoke path.

## Native tests

Compile the Windows test EXE after resource generation and transfer it to an authorized Windows host. The Go toolchain is not required on that host.

```powershell
$env:ROBOCOPY_EXPECT_ELEVATED = '0'
$env:ROBOCOPY_SCHEDULE_NATIVE_FIXTURE = '1'
.\native-tests.exe '-test.v' '-test.run=TestPrivilege|TestSchedule|TestMessageChoices|TestMessageDialogThemed'
```

The opt-in scheduling fixture creates/deletes its own future one-time/daily/weekly tasks and runs disposable worker copies. It must verify new registrations read back as `LeastPrivilege`, including when the registering process is deliberately elevated. Ordinary tests do not create scheduled tasks unless explicitly enabled. Never run against customer folders.

Test a genuinely non-administrator account as well as an administrator's filtered desktop token. A temporary local account, password-logon test harness and profile may be used only in an authorized VM with bounded cleanup. Such a disposable harness is not product credential storage; remove its exact task/account/profile afterward and verify absence. Do not commit credentials or private fixture transcripts.

## Interactive acceptance

Use an authorized logged-in desktop, not a WinRM service desktop. Launch the test harness with `InteractiveToken`/`LeastPrivilege` and independently inspect the app's real process-token elevation value.

- Require the title to indicate current user/not elevated and the explicit administrator-restart button to be available.
- Open the restart confirmation, choose No and verify the existing normal-privilege process stays open. Check safe default and failed/cancelled launch transitions separately.
- Fill disposable endpoints, run the scheduling dialog's unticked/default one-off action, hash-check copied content and confirm no task was created.
- Open the real native mini calendar (`DTM_GETMONTHCAL` returns its live HWND), inspect DD/MM/YYYY and HH:MM controls, then cancel without registering a job.
- Capture the real completed main window and calendar, inspect legibility, and verify clean process exit.
- Separately trigger the actual production EXE through a verified owned least-privilege task. Require matching source/destination hashes, Task Scheduler result 0, persisted preflight/copy history and task removal.

Run task-inventory comparisons serially: unrelated simultaneous fixture registration can create false failures. Walk controls may not implement UIAutomation ValuePattern; real HWND/class lookup and bounded system-marshalled WM_GETTEXT/WM_SETTEXT are alternatives. Do not pass sender-local pointers through cross-process WM_USER messages.

## Evidence limits

Cross-builds do not prove native behavior. A filtered admin token is not a substitute for testing a genuine non-admin account. Successful future-task registration does not prove multi-day/DST/logoff endurance. The application deliberately requires the user to be logged in for product schedules.

An automated safe-No check does not establish successful human approval of the secure-desktop UAC prompt. Windows owns that flow; cancellation/failure handling is unit tested, and real approval/alternate-account/network-share behavior must be accepted in the target environment. No SmartScreen or signing trust is claimed.
