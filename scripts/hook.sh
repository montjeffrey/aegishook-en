#!/usr/bin/env bash
# Installs/uninstalls client hooks only; no daemon, database or listening port.
set -euo pipefail
umask 077
action=''; agent='claude'; scope='global'; project=''; endpoint="${AEGIS_INSTALL_ENDPOINT:-}"; code_file=''
client_dir="${HOME:?HOME is required}/.aegishook-client"
source_binary=''; version='latest'; switch_scope=false; tmp=''
download_base="${AEGIS_INSTALL_DOWNLOAD_BASE:-}"
die() { printf '%s\n' "$*" >&2; exit 1; }
usage() {
 cat <<'EOF'
Usage: bash hook.sh install|uninstall [options]
  --agent pi|claude|codex|opencode|grok  default claude
  --scope global|project              default global
  --project /absolute/project         path for project scope
  --endpoint https://review.example   review service address (required for install)
  --code-file /private/enrollment-code  optional; if omitted, approve the first enrollment in the console
  --client-dir /private/client        default ~/.aegishook-client
  --switch                            on successful install, remove the other scope of the same Agent
  --binary /path/to/aegishook          use an existing binary (offline install / source testing)
  --version vX.Y.Z                     download a specific Release; default latest stable
  -h, --help                          show help
With no arguments, an install/uninstall menu is shown. This script does not install an Agent or start the review service.
EOF
}
if (( $#==0 )); then
 [[ -t 0 ]] || die 'Specify install or uninstall'
 printf '1. Install hook\n2. Uninstall hook\n'
 read -r -p 'Choose [1/2]: ' choice
 case "$choice" in 1) action=install;;2) action=uninstall;;*) die 'Choose 1 or 2';;esac
 read -r -p 'Agent [claude/pi/codex/opencode/grok, default claude]: ' choice; agent="${choice:-claude}"
 read -r -p 'Scope [global/project, default global]: ' choice; scope="${choice:-global}"
else
 case "$1" in install|uninstall) action="$1";shift;;-h|--help) usage;exit 0;;*) die 'Only install / uninstall are supported';;esac
fi
while (( $# )); do
 case "$1" in
  --agent|--scope|--project|--endpoint|--code-file|--client-dir|--binary|--version)
   (( $#>=2 )) && [[ -n "$2" && "$2" != --* ]] || die "$1 requires a value"
   case "$1" in
    --agent) agent="$2";;--scope) scope="$2";;--project) project="$2";;--endpoint) endpoint="$2";;
    --code-file) code_file="$2";;--client-dir) client_dir="$2";;--binary) source_binary="$2";;--version) version="$2";;
   esac;shift 2;;
  --switch) switch_scope=true;shift;;
  -h|--help) usage;exit 0;;
  *) die "Unknown option: $1";;
 esac
done
case "$agent" in pi|claude|codex|opencode|grok) ;;*) die 'Invalid Agent';;esac
case "$scope" in global|project) ;;*) die 'Scope must be global or project';;esac
if [[ "$scope" == project && -z "$project" && -t 0 ]]; then read -r -p 'Absolute project path: ' project;fi
[[ "$scope" != project || "$project" == /* ]] || die 'Project must be an absolute path'
[[ "$scope" != global || -z "$project" ]] || die 'The global scope cannot specify a project'
[[ "$client_dir" == /* && "$client_dir" != / && ! "$client_dir" =~ [[:cntrl:]] ]] || die 'Client directory must be an absolute path and not the root directory'
case "$(uname -s)" in Linux) os=linux;;Darwin) os=darwin;;*) die 'On Windows, use hook.ps1';;esac
case "$(uname -m)" in x86_64|amd64) arch=amd64;;arm64|aarch64) arch=arm64;;*) die 'Unsupported CPU architecture';;esac
if [[ "$action" == uninstall && ( -n "$code_file" || "$switch_scope" == true ) ]]; then die 'Uninstall does not take an enrollment code or --switch';fi
if [[ "$action" == install && -z "$endpoint" && -t 0 ]];then read -r -p 'Review service address: ' endpoint;fi
[[ "$action" != install || -n "$endpoint" ]] || die '--endpoint is required for install'
if command -v sha256sum >/dev/null 2>&1;then hash=(sha256sum);else hash=(shasum -a 256);fi
command -v "${hash[0]}" >/dev/null 2>&1 || die 'sha256sum or shasum is required'
binary="$client_dir/aegishook"
marker="$client_dir/binary.sha256"
cleanup(){ [[ -z "$tmp" ]] || rm -rf -- "$tmp"; }
trap cleanup EXIT
if [[ ! -e "$binary" ]]; then
 if [[ "$action" == uninstall && ! -e "$client_dir/client.json" ]];then printf 'No hook is registered for this scope.\n';exit 0;fi
 if [[ -z "$source_binary" && -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]];then
  script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
  if [[ -x "$script_dir/../aegishook" ]];then source_binary="$script_dir/../aegishook";fi
 fi
 tmp="$(mktemp -d "${TMPDIR:-/tmp}/aegishook-client.XXXXXXXX")"
 if [[ -z "$source_binary" ]];then
  [[ "$action" == install ]] || die 'Hook binary is missing; use --binary to supply the original version binary so uninstall can proceed'
  command -v curl >/dev/null 2>&1 || die 'curl is required'
  curl_args=(--fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 300 --retry 2)
  if [[ -n "$download_base" ]];then
   if [[ "$download_base" =~ ^http://(127\.0\.0\.1|localhost|\[::1\])(:[0-9]+)?/ ]];then
    curl_args=(--fail --silent --show-error --proto '=http' --connect-timeout 15 --max-time 300 --retry 2)
   else
    [[ "$download_base" == https://* && "$download_base" != *'@'* ]] || die 'Binary download requires HTTPS or loopback HTTP'
    curl_args=(--fail --silent --show-error --proto '=https' --connect-timeout 15 --max-time 300 --retry 2)
   fi
   curl "${curl_args[@]}" -o "$tmp/aegishook" "$download_base/bin/$os/$arch"
   expected="$(curl "${curl_args[@]}" "$download_base/sha256/$os/$arch")"
   actual="$("${hash[@]}" "$tmp/aegishook" | awk '{print $1}')"
   [[ "$expected" =~ ^[0-9a-f]{64}$ && "$expected" == "$actual" ]] || die 'Client binary SHA-256 verification failed'
  else
  release='https://github.com/montjeffrey/aegishook-en/releases'
  no_release="No release binary could be downloaded from $release. This edition may not have published binaries yet: build it from source and pass --binary <path>, or run the service with --client-binaries so it serves the binary itself."
  if [[ "$version" == latest ]];then
   resolved="$(curl "${curl_args[@]}" -o /dev/null -w '%{url_effective}' "$release/latest")" || die "$no_release"
   [[ "$resolved" == "$release/tag/"* ]] || die "$no_release (GitHub has no latest release to resolve)"
   version="${resolved#"$release/tag/"}"
  fi
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die 'Invalid version format'
  package="aegishook_${version}_${os}_${arch}"; archive="$package.tar.gz"
  curl "${curl_args[@]}" -o "$tmp/$archive" "$release/download/$version/$archive" || die "$no_release"
  curl "${curl_args[@]}" -o "$tmp/SHA256SUMS.txt" "$release/download/$version/SHA256SUMS.txt" || die "$no_release"
  expected="$(awk -v name="$archive" '$2==name || $2=="*"name {print $1}' "$tmp/SHA256SUMS.txt")"
  actual="$("${hash[@]}" "$tmp/$archive" | awk '{print $1}')"
  [[ "$expected" =~ ^[0-9a-f]{64}$ && "$expected" == "$actual" ]] || die 'Release package SHA-256 verification failed'
  tar -xOzf "$tmp/$archive" "$package/aegishook" > "$tmp/aegishook"
  fi
  source_binary="$tmp/aegishook";chmod 700 "$source_binary"
 fi
 [[ -f "$source_binary" ]] || die 'Cannot find the binary given by --binary'
 # Check protocol capability before creating any Hook configuration.
 "$source_binary" client install -h >/dev/null || die 'The binary does not support client install; use a version that includes the client command'
 mkdir -p -- "$client_dir"
 [[ ! -L "$binary" && ! -e "$binary" && ! -L "$marker" && ! -e "$marker" ]] || die 'Binary or ownership marker already exists; not overwritten'
 cp -- "$source_binary" "$client_dir/.aegishook-new"
 chmod 700 "$client_dir/.aegishook-new"
 mv -- "$client_dir/.aegishook-new" "$binary"
 "${hash[@]}" "$binary" | awk '{print $1}' > "$marker"
fi
[[ ! -L "$binary" && ! -L "$marker" && -f "$marker" ]] || die 'Binary ownership cannot be confirmed; not modified'
[[ "$("${hash[@]}" "$binary" | awk '{print $1}')" == "$(cat "$marker")" ]] || die 'Client binary has been modified; not executed'
args=(client "$action" --client-dir "$client_dir" --agent "$agent" --scope "$scope")
if [[ -n "$project" ]];then args+=(--project "$project");fi
if [[ "$action" == install ]];then
 args+=(--endpoint "$endpoint")
 if "$switch_scope";then args+=(--switch);fi
 if [[ -n "$code_file" ]];then
  "$binary" "${args[@]}" --code-file "$code_file"
 else
  "$binary" "${args[@]}"
 fi
else
 "$binary" "${args[@]}"
 if [[ ! -e "$client_dir/client.json" ]];then
  rm -- "$binary" "$marker"
  printf 'All hooks uninstalled. Lock files and files not owned by AegisHook in the client directory are kept.\n'
 fi
fi
