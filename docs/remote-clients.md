# Remote Hook installation and uninstallation

Clients use a native script entry point: `scripts/hook.sh` on macOS / Linux and `scripts/hook.ps1` on Windows (PowerShell 5.1+). Cross-platform config merging, enrollment requests, file ownership and uninstallation are all handled by the same Go program, so Python is not needed. The scripts only install and uninstall Hooks; they do not install the Agent and do not create background services, databases or listening ports. The Agent calls the risk-control service on every tool event.

## First-time enrollment

In the console, open **Agent integration -> Remote devices**, expand the install command, then download and run it on the target machine. A request is valid for ten minutes by default, and the install terminal shows the request ID. The administrator checks the full ID, device name, system and source IP, then chooses allow, reject, or reject and block the IP. The name and platform are self-reported by the client and cannot serve alone as trusted identity. Nothing is written to the Agent Hook before approval. If the installer exits midway, run it again to keep waiting on the request that has not expired.

The download URL has the form `https://review.example/install/<random-path>/hook.sh` and stays stable for each service data directory. The script contains no admin token, shared Hook token or model API key.

```bash
curl -fsS 'https://review.example/install/<random-path>/hook.sh' -o hook.sh
# wget -O hook.sh 'https://review.example/install/<random-path>/hook.sh'
bash hook.sh install --agent claude --scope global

# After confirming the download source, you can also pipe it straight to the shell
curl -fsS 'https://review.example/install/<random-path>/hook.sh' | bash -s -- install --agent claude --scope global

# Switch to the current project only
bash hook.sh install --agent claude --scope project --project /absolute/project --switch
bash hook.sh uninstall --agent claude --scope project --project /absolute/project
```

```powershell
Invoke-WebRequest 'https://review.example/install/<random-path>/hook.ps1' -OutFile hook.ps1
.\hook.ps1 -Action install -Agent claude -Scope global
.\hook.ps1 -Action install -Agent claude -Scope project -Project C:\work\project -SwitchScope
.\hook.ps1 -Action uninstall -Agent claude -Scope project -Project C:\work\project
```

Global means the current user and needs no sudo or administrator rights. The default client directory is `~/.aegishook-client`; if you customize it, pass `--client-dir` / `-ClientDir` every time. Supported agents are claude, pi, codex, opencode and grok; each Agent's own Hook trust and reload requirements still apply. On Windows, Pi symlinks need Developer Mode or the matching permission. Release architectures are macOS/Linux amd64 and arm64, and Windows amd64.

Repeated installation keeps the same device identity; when `--switch` succeeds it removes the other kind of scope for that Agent, and switching from project to global removes all project Hooks of the same kind registered by that client. Grok allows only a single installation scope. Uninstalling removes only registered config entries that have not been modified and keeps other settings. Uninstalling the last entry deletes the owned program and connection files; the stable lock file and files the user has modified may remain. Uninstalling can be done offline and does not automatically revoke the device identity in the console; to block the device, also click **Revoke access**.

## Service address and script distribution

When this machine is on an internal network and the test machine is on the public internet, you can open a reverse SSH tunnel from this machine:

```sh
ssh -N -o ExitOnForwardFailure=yes \
  -R 127.0.0.1:18790:127.0.0.1:18790 user@public-server
```

The remote machine then downloads the script and sends Hook requests through its own `http://127.0.0.1:18790`, and the traffic is forwarded over SSH to the console on this machine; the public server does not need to connect to this machine's internal address. Port 18790 on the remote side must be free, and the SSH server must allow remote forwarding. The forward listens only on the remote loopback address, and it closes when you exit SSH after testing. While the tunnel is in use, the console does not need `--public-url`; do not treat this temporary connection as a long-running service.

Through an SSH tunnel, the console sees a loopback source, so the original IP of the public machine cannot be identified from it. In this case, revoke access by device credential; blocking the loopback IP would affect other Hooks connecting through the loopback address.

By default only loopback HTTP on this machine is supported. Plain remote HTTP would leak credentials and request content, so enrollment must use HTTPS, or use an SSH tunnel and have the target machine request its own `127.0.0.1`. The random path is for distribution and is not an authentication credential.

The service still listens only on the loopback address. Configure an HTTPS reverse proxy that keeps the original Host, then declare the public origin when starting the service. The following is a deployment example; running it locally from this repository must also follow the data directory, single-instance and port conventions in `local-development.md`.

```sh
./aegishook serve --addr 127.0.0.1:18790 --data-dir /private/aegis-data \
  --public-url https://review.example --trusted-proxies 127.0.0.1/32 \
  --client-binaries /private/aegis-clients
```

The reverse proxy should terminate TLS with a valid certificate, pass `Host`, and overwrite `X-Real-IP` with the client address it actually observed. `--trusted-proxies` should list only the exact CIDRs of controlled proxies and must not be set to the whole internet. When it is not configured, all source-IP forwarding headers are ignored; in a proxy setup, what gets blocked would be the proxy address. The IP blocklist rejects enrollment requests and Hook requests from that source, may affect other devices behind a shared egress, and does not replace device credentials.

`--client-binaries` is a read-only distribution directory of self-hosted raw executables, with the file names below. The distribution endpoint serves only these files and their SHA-256 checksums, and does not expose the service data directory.

| System | File names |
|---|---|
| macOS | `aegishook-darwin-arm64`, `aegishook-darwin-amd64` |
| Linux | `aegishook-linux-arm64`, `aegishook-linux-amd64` |
| Windows | `aegishook-windows-amd64.exe` |

First build and prepare the embedded pages (`npm run build`), then cross-compile the files you need with Go, for example `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /private/aegis-clients/aegishook-linux-arm64 ./cmd/aegishook`. The client program must come from a version compatible with the server. When the distribution directory is omitted, the script downloads the GitHub Release and verifies its SHA-256; you must use a newer version that includes the `client` subcommand, because older versions refuse to install.

You can also use `scripts/hook.sh` / `hook.ps1` from an already downloaded release package and specify the address with `--endpoint` / `-Endpoint`; the script then uses the program from the same package. `--binary` / `-Binary` points to an existing trusted program.

## Credentials and boundaries

- A leaked install script grants no access. Human approval is required by default; after approval, the separate device credential can access only its own sessions, reviews and execution results, and cannot manage rules, read other devices' sessions or approve its own enrollment. The service stores only a digest of the credential, which can be revoked individually.
- For unattended scenarios, a logged-in administrator can call `POST /api/v1/client-enrollments` to create a one-time enrollment code valid for ten minutes and pass it through a private file to `--code-file` / `-CodeFile`. The enrollment code itself grants one enrollment and should not be put in a URL, chat log, shared script or log.
- The local device credential is stored in the client's private directory. Unix uses a private directory and 0600 files; the Windows script sets the current user's ACL. Do not copy `connection.json`, `enrolled.json` or `request.json`; anyone holding the device credential can impersonate the device and must be revoked in the console.
- SHA-256 verifies download integrity and cannot protect against the distribution server itself being fully compromised. The script and checksum downloads need trusted HTTPS; do not use options that skip certificate verification. For stronger supply-chain guarantees, you can separately adopt signed releases and offline verification.
- A Hook is a control point provided by the Agent, not an operating system sandbox. Remote authorization scope is interpreted by that device's path semantics and symlinks are not resolved on the console machine; with symlinks, remote file systems or a client that disables Hooks, it must not be treated as a strong file isolation boundary. The client's original permission checks continue to apply.
