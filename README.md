# mw-watch

Tracks how long your computer's screen is on and how long you actively use it. It runs on Windows and macOS, and the stats stay on your machine.

Time is recorded in 1-minute buckets:

- **Active**: there was at least one mouse or keyboard input during the minute.
- **Screen on**: at some point during the minute, the display was on and the session was unlocked. Time on the lock screen doesn't count.

The stats are shown at <http://localhost:47823> as charts with a step from 1 minute to 1 month.

## Install

Build the binaries (requires Go; no C compiler needed):

```powershell
./build.ps1        # Windows
```
```sh
./build.sh         # macOS / Linux shell
```

This writes `dist/mw-watch.exe`, `dist/mw-watch-darwin-arm64` (Apple Silicon) and `dist/mw-watch-darwin-amd64` (Intel Mac). Copy the one you need somewhere permanent, then register it to start at login:

```sh
mw-watch install
```

- **Windows:** adds a `mw-watch` entry under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` and starts the tracker now.
- **macOS:** installs `~/Library/LaunchAgents/com.mw-watch.plist` and loads it.

If you move the executable, run `install` again. To stop autostart and stop the running tracker:

```sh
mw-watch uninstall
```

Your recorded data is kept.

## Commands

| Command | What it does |
| --- | --- |
| `mw-watch` or `mw-watch run` | Track in the foreground and serve the UI |
| `mw-watch install` | Start at login, and start now |
| `mw-watch uninstall` | Remove autostart and stop running copies |
| `mw-watch probe` | Print idle time, display state and lock state every second, for troubleshooting |

`run` and `install` accept:

- `--addr`: the web UI address. The default is `127.0.0.1:47823`. Only loopback addresses are accepted.
- `--data-dir`: where the database and log go.

On Windows the binary has no console window. To see the output of `probe`, pipe it through something, for example `mw-watch.exe probe | more`.

## Where the data lives

| OS | Directory |
| --- | --- |
| Windows | `%LOCALAPPDATA%\mw-watch\` |
| macOS | `~/Library/Application Support/mw-watch/` |

`mw-watch.db` is a SQLite database with one row per minute: `buckets(start, active, screen_on)`, where `start` is a Unix timestamp. `mw-watch.log` is the log file.

## Privacy

- The web server listens on loopback only and refuses to start on any other address.
- It answers only requests addressed to `localhost`, `127.0.0.1` or `::1`, which blocks DNS-rebinding attacks from web pages.
- The UI is embedded in the binary and loads nothing from the internet. The page's Content-Security-Policy is `default-src 'self'`.
- mw-watch makes no outbound network connections. It records only timestamps and two flags, never which keys were pressed or what was on screen.

## How it measures

Every 10 seconds, aligned to the clock, mw-watch reads:

- **Idle time:**
  - Windows: `GetLastInputInfo`
  - macOS: `CGEventSourceSecondsSinceLastEventType`. This needs no Accessibility or Input Monitoring permission.
- **Display power:**
  - Windows: `GUID_CONSOLE_DISPLAY_STATE` power notifications. A dimmed display counts as on.
  - macOS: `CGDisplayIsAsleep` on the main display.
- **Lock state:**
  - Windows: `WTS_SESSION_LOCK` and `WTS_SESSION_UNLOCK` notifications.
  - macOS: `CGSSessionScreenIsLocked`.

A minute is marked active if input happened since the previous reading. Nothing is recorded while the computer sleeps. After waking, input from before the sleep is not counted.
