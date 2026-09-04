# meeting-record

`meeting-record` is a small Linux CLI that passively records two independent
PipeWire tracks:

- `local.flac`: the current WirePlumber default microphone (48 kHz, mono)
- `remote.flac`: the monitor ports of the current default output sink (48 kHz,
  stereo), including every application and system sound routed there
- `meeting.m4a`: a finalized 48 kHz stereo AAC mix for playback and export

It does not create virtual devices, change defaults, reroute streams, or insert
itself between applications and hardware. Google Meet continues to use the
physical microphone and output normally while the recorder observes the same
PipeWire graph.

## Requirements

- Linux with PipeWire and WirePlumber
- `wpctl`
- `pw-record` with FLAC container support
- `ffmpeg` for the post-recording mix
- the official Notion CLI (`ntn`) only when exporting to Notion

The Nix package wraps the executable with these runtime tools in `PATH` and
also exposes the packaged official CLI as `.#notion-cli`.

## Build

```console
nix build
nix develop -c go test ./...
```

## CLI

```console
meeting-record devices
meeting-record devices --json
meeting-record start
meeting-record start --detach
meeting-record stop
meeting-record status --json
meeting-record list --json
meeting-record show SESSION --json
meeting-record open SESSION
meeting-record play SESSION meeting
meeting-record play SESSION local
meeting-record play SESSION remote
meeting-record mix SESSION
meeting-record upload SESSION
meeting-record delete SESSION
```

Foreground `start` owns both `pw-record` children until Ctrl-C or SIGTERM.
`start --detach` starts the same Go supervisor in its own transient systemd user
service and returns only after both recorders survive their startup grace period.
That separate cgroup is what lets it survive a Quickshell service restart. On a
host without a usable user systemd manager, it falls back to a new Unix session.
The supervisor—not the launcher or a desktop UI—continues to own both children. It uses process groups,
sends SIGINT first so FLAC containers finalize, waits, and escalates only if a
child refuses to exit. If either track exits unexpectedly, the other is stopped
and the session is marked failed.

After both capture processes finalize, the supervisor runs `ffmpeg` to create
`meeting.m4a`. It upmixes the microphone to stereo, mixes it with the complete
output-sink track, and applies a limiter. The source tracks remain untouched.
`meeting-record mix SESSION` can rebuild the mixed file for an older session.

Only one session can be active. An advisory runtime lock closes start races.
`stop` sends a tiny command over a mode-0600 Unix socket and waits for both files
and metadata to finalize.

## Capture details

Device discovery runs:

```console
wpctl inspect @DEFAULT_AUDIO_SOURCE@
wpctl inspect @DEFAULT_AUDIO_SINK@
```

The stable `node.name` and human-readable `node.description` are retained. The
local stream targets the source by name with `stream.monitor=true`. The remote
stream targets the sink by name with `stream.capture.sink=true`; WirePlumber
then links it to the sink's existing `monitor_*` output ports. This is passive
observation and does not create a virtual monitor source.

The commands were verified against PipeWire 1.6.8 / WirePlumber 1.6.6. Always
check `pw-record --help` and `pw-record --list-containers` when using a materially
different host version.

## Files and state

Completed and active sessions live under:

```text
${XDG_DATA_HOME:-$HOME/.local/share}/meeting-record/SESSION/
├── meeting.json
├── local.flac
├── remote.flac
└── meeting.m4a
```

Ephemeral state lives under `$XDG_RUNTIME_DIR/meeting-record/` (with a private
per-UID `/tmp` fallback):

```text
state.json
control.sock
supervisor.lock
supervisor.log
startup-error
```

`state.json` is atomically replaced only at state transitions; it is not written
every second. `status` verifies an active state against the socket rather than
trusting the file. If a supervisor was killed, the next status reconciliation
marks runtime state idle and finalizes the session metadata as failed.

## Quickshell boundary

The companion Quickshell integration belongs in the desktop configuration and
uses only short-lived CLI commands plus a watched `state.json`. Quickshell never
owns `pw-record`, so closing a popup, changing monitors, reloading the config, or
crashing the shell cannot stop a recording. A one-second QML timer derives
elapsed time from `startedAt`; it does not poll or write runtime state.

## Notion meeting notes

`meeting-record upload SESSION` passes `meeting.m4a` to the official `ntn`
CLI, then creates a native Notion AI meeting-notes block from the completed
upload. The Go process uses explicit argument arrays and streams the file to
`ntn` over stdin; it does not contain Notion credentials or make its own HTTP
requests. A successful block ID is saved in `meeting.json`, and a second upload
is refused to avoid duplicate meeting-note blocks.

Authenticate once with:

```console
ntn login
```

Choose the Notion page that will contain new meeting-note blocks and expose its
ID to the CLI and Quickshell service:

```console
export MEETING_RECORD_NOTION_PARENT_PAGE_ID=0123456789abcdef0123456789abcdef
meeting-record upload SESSION
```

Alternatively, pass `--parent-page ID` for a single terminal upload. The
integration requests automatic language detection and Notion's normal summary
generation. The current Notion public API and `ntn` endpoint catalog do not
provide a Notion Calendar API or accept calendar-event data when creating a
meeting note, so this project deliberately performs no Google/Gmail calendar
integration.

## Manual integration test

1. Run `meeting-record devices` and confirm the physical microphone and desired
   output sink are selected.
2. Start Google Meet using those normal devices.
3. Run `meeting-record start` (or start detached and later use `stop`).
4. Speak into the microphone and play remote/system audio through the output.
5. Confirm Meet's microphone and headphones continue to work without a device
   switch or a new virtual device.
6. Stop the recorder cleanly.
7. Play `local.flac` and verify it contains microphone audio.
8. Play `remote.flac` and verify it contains exactly the complete sink output,
   including any intentionally generated notification, music, or browser audio.
9. Play `meeting.m4a` and verify it contains both sources.
10. Inspect `meeting.json` and verify timestamps, devices, status, and duration.

For a shell-lifecycle test, start with `--detach`, reload or kill/restart
Quickshell, verify `status --json` remains active, then stop from the reloaded
shell and confirm both FLAC files are readable.
