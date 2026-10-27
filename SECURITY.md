# Security and data handling

RoboCopy Studio executes powerful native filesystem operations. Use only on data and systems you are authorized to manage. Default copies do not delete destination extras; destructive modes remain opt-in and require confirmation.

## Privilege model

The executable requests `asInvoker` and does not demand administrator rights at startup. The UI reports the actual token state. Explicit **Restart as administrator…** uses Windows' `runas` consent flow with the current executable's absolute path, no shell, and no forwarded commands or endpoints. No credentials are collected by the application. Declined consent, UAC cancellation and failed launches do not close the current window. A successful restart starts a blank form and discards prior preview approval.

New tasks use current-SID `InteractiveToken`/`LeastPrivilege`; legacy verified `HighestAvailable` tasks remain identifiable without silently changing their privilege. This is not a privilege-escalation service, credential broker or unattended elevated execution mechanism. Enterprise policy, endpoint protection and filesystem/share ACLs remain authoritative.

## Boundaries

- The EXE is unsigned. Store it in a trusted location, verify release checksums and follow organizational application-control policy.
- Managed paths are validated lexically and through native handles/ancestors, but previews are not immutable snapshots. Same-account tampering, malicious administrators, concurrent reparse changes and remote aliases are not fully isolated.
- Expert/JOB options are an explicit direct-engine boundary, not a sandbox. Do not execute untrusted job files.
- Logs, profiles and manifests can contain sensitive paths/output/arguments and are not encrypted. Scheduled snapshots use current-user DPAPI, which does not isolate other processes acting as that account. Clipboard/export destinations can retain data outside the application store.
- Capture, preview and store budgets fail explicitly rather than silently pruning or claiming complete evidence. Native cancellation may be delayed by blocking filesystem/network operations.
- Native smoke/GUI tests and source hardening are not a comprehensive security sign-off. A previously paused broader audit remains unfinished; no zero-vulnerability or leak-free guarantee is made.

## Reporting

Report suspected vulnerabilities privately to the repository maintainer through an agreed private channel. Do not publish credentials, customer paths, complete sensitive logs or exploit details in a public issue. No reporting address is invented by this handoff; the repository owner should configure a private reporting channel before public release.

Include the release checksum, Windows build, minimal benign reproduction and whether the app was elevated. Use disposable fixture data whenever possible.
