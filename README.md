# DumpDuck

DumpDuck is a macOS-oriented background TCP dump service for capturing traffic into rotating files and uploading completed windows to remote storage. Phase 2 implements the foreground runtime loop for `dumpduck run`, including tcpdump process management, periodic uploads with rclone, and stateful restart recovery. LaunchDaemon installation is still deferred.

## Commands

Build or run the CLI from the repository root:

```bash
go run ./cmd/dumpduck --help
```

Available commands:

```text
dumpduck config init --path <path> [--force]
dumpduck config set <key> <value> --path <path>
dumpduck status --config <path>
dumpduck run --config <path>
dumpduck install
dumpduck uninstall
```

### `config init`

Creates a YAML config file with DumpDuck defaults, including parent directories when needed. Existing files are preserved unless `--force` is provided.

Default config path:

```text
/etc/dumpduck/config.yaml
```

### `config set`

Updates supported config keys in place and persists them back to YAML. Phase 1 supports:

```text
upload.frequency
upload.rclone_remote
upload.rclone_path
capture.interface
capture.bpf_filter
capture.output_dir
state.path
logging.path
```

Duration values are validated with Go duration syntax such as `15m`, `1h`, or `24h`.

### `status`

Loads the YAML config and the JSON state file if present, then prints:

- config path
- dump directory
- upload frequency
- rclone destination
- state path
- last successful upload time or `never`
- current window start time or `none`

### `run`

`dumpduck run --config <path>` now performs the real capture-window runtime:

- loads and validates YAML config
- loads JSON state from `state.path`
- starts a new 24-hour capture window if none is active
- resumes an existing active window after a restart
- uploads completed `.pcap` files every `upload.frequency`
- stops `tcpdump` and performs one final upload pass when the window expires or the process receives `SIGINT`/`SIGTERM`
- persists uploaded-file records and the current window start so restarts do not re-upload completed files

If DumpDuck starts after the saved window has already expired, it performs one upload pass for any completed pending dumps, clears the expired window marker, prints a message, and exits without starting `tcpdump`.

`tcpdump` is invoked with:

- `binaries.tcpdump_path`
- optional `-i <interface>`
- optional `-s <snaplen>` when `snaplen > 0`
- `-G <rotate-interval-seconds>`
- `-w <output_dir>/dumpduck-%Y%m%d-%H%M%S.pcap`

`capture.bpf_filter` is appended using simple whitespace splitting. Quoted filter fragments are not supported yet.

DumpDuck skips uploading the file that is still likely being written by ignoring `.pcap` files newer than one rotate interval. Uploaded files are recorded in the JSON state file, and optionally deleted locally when `upload.delete_after_success` is enabled.

Real packet capture still depends on `tcpdump` permissions on the host. The test suite uses fake binaries and does not require root.

### `install` / `uninstall`

These are still placeholders. LaunchDaemon installation is planned for a later phase.

## Default configuration

The generated YAML config carries these defaults:

```yaml
capture:
  interface: ""
  bpf_filter: ""
  snaplen: 0
  output_dir: /var/lib/dumpduck/dumps
  rotate_interval: 5m
  total_duration: 24h
upload:
  frequency: 15m
  rclone_remote: gdrive
  rclone_path: dumpduck
  delete_after_success: false
state:
  path: /var/lib/dumpduck/state.json
logging:
  path: /var/log/dumpduck/dumpduck.log
binaries:
  tcpdump_path: /usr/sbin/tcpdump
  rclone_path: /opt/homebrew/bin/rclone
```

## State file

The JSON state file tracks:

- last successful upload time
- uploaded file records
- current window start time

The default path is:

```text
/var/lib/dumpduck/state.json
```

## Remaining work

Later phases will add:

- LaunchDaemon installation and removal for macOS service management
- background service wiring around the existing `run` behavior
