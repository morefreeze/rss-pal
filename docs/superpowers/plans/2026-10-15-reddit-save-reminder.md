# Reddit save reminder
Confirmed: only Reddit post pages, cumulative foreground viewing for five minutes, one hidden 60% draw per post, inline reminder. Closing permanently disables reminders for all posts. Explicit save uses current captured HTML; does not expand comments. No new permissions.

- Implement URL/state/worker handler with deterministic tests: foreground timing, persistent decision, probability boundary, disabled preference, source checks and save errors.
- Add Reddit content script with focus/visibility/SPA tracking and isolated accessible reminder UI. Do not read credentials in content scripts; worker saves via existing capture endpoint. No automatic overwrites.
- Wire worker, manifest and extension version. Verify extension regression suite and package. Review, integrate newest master, push, verify Tencent artifact revision and published ZIP. Installed extension requires reload/update; verify what is actually installed before claims.
