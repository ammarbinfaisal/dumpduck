# DumpDuck

DumpDuck is a macOS background `tcpdump` service managed with LaunchDaemon, configured with YAML, stateful through a JSON state file, and able to move completed capture windows to remote storage with periodic `rclone` runs.

## Commands

Build or run the CLI from the repository root:

```bash
go run ./cmd/dumpduck --help
```

Before `install`, build a stable binary so the LaunchDaemon does not point at a temporary `go run` artifact:

```bash
go build -o ./bin/dumpduck ./cmd/dumpduck
```

Available commands:

```text
dumpduck config init --path <path> [--force]
dumpduck config set <key> <value> --path <path>
dumpduck status --config <path>
dumpduck run --config <path>
dumpduck install [--config <path>] [--binary <path>] [--plist <path>] [--dry-run] [--skip-load]
dumpduck uninstall [--plist <path>] [--dry-run] [--skip-unload]
```

### `config init`

Creates a YAML config file with DumpDuck defaults, including parent directories when needed. Existing files are preserved unless `--force` is provided.

Default config path:

```text
/etc/dumpduck/config.yaml
```

### `config set`

Updates supported config keys in place and persists them back to YAML. Supported keys:

```text
upload.frequency
upload.rclone_config_path
upload.rclone_remote
upload.rclone_path
capture.interface
capture.bpf_filter
capture.output_dir
state.path
logging.path
```

Duration values are validated with Go duration syntax such as `15m`, `1h`, or `24h`.

`upload.rclone_path` is the destination path inside the configured remote, not the local `rclone` executable path. The executable path stays in the YAML config as `binaries.rclone_path`.

`upload.rclone_config_path` is optional. When set, DumpDuck passes it to rclone as `--config <path>`, which is useful for LaunchDaemon installs because the service runs as root and will otherwise look for root's default rclone config instead of your interactive user's config.

### `status`

Loads the YAML config and the JSON state file if present, then prints:

- config path
- dump directory
- upload frequency
- upload destination
- rclone config path, or `rclone default` when unset
- state path
- last successful upload time or `never`
- current window start time or `none`
- LaunchDaemon label
- whether the default LaunchDaemon plist path exists

### `run`

`dumpduck run --config <path>` now performs the real capture-window runtime:

- loads and validates YAML config
- loads JSON state from `state.path`
- starts a new 24-hour capture window if none is active
- resumes an existing active window after a restart
- moves completed `.pcap` files to remote storage every `upload.frequency`
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

DumpDuck skips moving the file that is still likely being written by ignoring `.pcap` files newer than one rotate interval. Uploaded files are recorded in the JSON state file after a successful `rclone move`; the local source file is removed by rclone.

Real packet capture still depends on `tcpdump` permissions on the host. The test suite uses fake binaries and does not require root.

### `install`

`dumpduck install` prepares a macOS LaunchDaemon for DumpDuck using the label:

```text
com.dumpduck.service
```

Default paths:

```text
config: /etc/dumpduck/config.yaml
plist:  /Library/LaunchDaemons/com.dumpduck.service.plist
```

The generated LaunchDaemon runs:

```text
<absolute dumpduck binary path> run --config <config path>
```

Supported flags:

- `--config <path>`: config file path. If the file does not exist, DumpDuck creates a default config there before writing the plist.
- `--binary <path>`: binary path to run. Defaults to the current executable when it can be discovered. For a real install, build DumpDuck first and point `--binary` at that stable path. DumpDuck rejects an implicit `go run` build artifact to avoid writing a LaunchDaemon plist that points at a temporary binary.
- `--plist <path>`: LaunchDaemon plist path.
- `--dry-run`: prints the generated plist to stdout and does not write files or call `launchctl`.
- `--skip-load`: writes the plist but skips `launchctl bootstrap system <plist>`.

During a real install, DumpDuck creates the config directory, capture output directory, state directory, logging directory, and plist parent directory as needed.

Real installs normally need `sudo` because both `/etc/dumpduck` and `/Library/LaunchDaemons` are root-owned on macOS:

```bash
sudo dumpduck install
```

If you are not running an already installed `dumpduck` binary, build one first and point install at it:

```bash
go build -o ./bin/dumpduck ./cmd/dumpduck
sudo ./bin/dumpduck install --binary "$(pwd)/bin/dumpduck"
```

Preview the plist safely:

```bash
dumpduck install --dry-run --config /tmp/dumpduck.yaml --binary /usr/local/bin/dumpduck
```

Write the plist without loading it yet:

```bash
sudo dumpduck install --skip-load
```

## Service registration scripts

The `scripts/` directory includes wrappers for registering DumpDuck as a background service.

### macOS LaunchDaemon

Register or refresh the macOS LaunchDaemon with:

```bash
DUMPDUCK_RCLONE_CONFIG="$HOME/.config/rclone/rclone.conf" ./scripts/register-launchctl.sh
```

The script builds `./bin/dumpduck`, writes `/Library/LaunchDaemons/com.dumpduck.service.plist`, updates `upload.rclone_config_path` when `DUMPDUCK_RCLONE_CONFIG` is set, bootstraps the service with `launchctl`, and prints `dumpduck status`.

Optional overrides:

```text
DUMPDUCK_BINARY=/absolute/path/to/dumpduck
DUMPDUCK_CONFIG=/etc/dumpduck/config.yaml
DUMPDUCK_PLIST=/Library/LaunchDaemons/com.dumpduck.service.plist
DUMPDUCK_RCLONE_CONFIG=/absolute/path/to/rclone.conf
```

### Linux systemd

Register or refresh a systemd service with:

```bash
DUMPDUCK_RCLONE_CONFIG="$HOME/.config/rclone/rclone.conf" ./scripts/register-systemctl.sh
```

The script builds `./bin/dumpduck`, creates `/etc/dumpduck/config.yaml` if needed, writes `/etc/systemd/system/dumpduck.service`, runs `systemctl daemon-reload`, enables and restarts the service, then prints `systemctl status`.

Optional overrides:

```text
DUMPDUCK_BINARY=/absolute/path/to/dumpduck
DUMPDUCK_CONFIG=/etc/dumpduck/config.yaml
DUMPDUCK_SYSTEMD_UNIT=/etc/systemd/system/dumpduck.service
DUMPDUCK_RCLONE_CONFIG=/absolute/path/to/rclone.conf
```

### `uninstall`

`dumpduck uninstall` removes the LaunchDaemon plist and, unless told otherwise, unloads the service first with:

```text
launchctl bootout system/com.dumpduck.service
```

Supported flags:

- `--plist <path>`: LaunchDaemon plist path.
- `--dry-run`: prints what would be unloaded and removed.
- `--skip-unload`: removes the plist without calling `launchctl bootout`.

Examples:

```bash
dumpduck uninstall --dry-run
sudo dumpduck uninstall
sudo dumpduck uninstall --skip-unload
```

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
  rclone_config_path: ""
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
- uploaded file records with local path, remote path, upload time, byte size, and SHA1
- current window start time

The default path is:

```text
/var/lib/dumpduck/state.json
```

## Future work

Possible future improvements:

- deeper service-health reporting beyond plist presence
- more operational tooling around the existing background service
