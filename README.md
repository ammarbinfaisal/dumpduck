# DumpDuck

DumpDuck is a macOS-oriented background TCP dump service for capturing traffic into rotating files and uploading completed windows to remote storage. Phase 1 lays down the Go project foundation, typed configuration and state handling, and a small CLI. Runtime orchestration with `tcpdump`, `rclone`, and `launchd` is intentionally deferred.

## Phase 1 commands

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

Validates the config and exits with a phase-1 placeholder message. No background capture or upload loop is started yet.

### `install` / `uninstall`

These are placeholders in phase 1. LaunchDaemon installation is planned for a later phase.

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

Phase 1 introduces a JSON state file for later runtime phases. It currently tracks:

- last successful upload time
- uploaded file records
- current window start time

The default path is:

```text
/var/lib/dumpduck/state.json
```

## Planned daemon behavior

Later phases will add:

- foreground and background runtime orchestration around `tcpdump`
- periodic upload windows via `rclone`
- stateful recovery across restarts
- `launchd` installation and removal for macOS service management
