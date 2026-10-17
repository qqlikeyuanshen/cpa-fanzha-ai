#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 1 || "$1" != /* || "$(uname -s)" != Linux ]]; then
  echo '用法：bash install.sh /你的CPA插件扫描目录/linux/amd64（Linux x86_64）' >&2
  exit 1
fi
for cmd in curl sha256sum realpath install mktemp ln getconf; do
  command -v "$cmd" >/dev/null || { echo "缺少依赖：$cmd" >&2; exit 1; }
done
[[ "$(uname -m)" == x86_64 ]] || { echo '当前 Release 仅提供 Linux amd64 成品。' >&2; exit 1; }
libc="$(getconf GNU_LIBC_VERSION 2>/dev/null || true)"
if [[ "$libc" =~ ^glibc\ ([0-9]+)\.([0-9]+)$ ]]; then
  major="${BASH_REMATCH[1]}"; minor="${BASH_REMATCH[2]}"
  (( major > 2 || major == 2 && minor >= 35 )) || { echo '当前成品需要 glibc 2.35 或更新版本。' >&2; exit 1; }
else
  echo '当前成品需要 glibc Linux，不支持 Alpine/musl。' >&2; exit 1
fi
target="$(realpath -m -- "$1")"
[[ "$target" != / ]] || { echo '不能将根目录作为插件目录。' >&2; exit 1; }
if [[ -e "$target" && ! -d "$target" ]]; then
  echo '目标不是目录。' >&2; exit 1
fi
shopt -s nullglob dotglob
existing=("$target"/*fanzha*.so)
if (( ${#existing[@]} )); then
  echo '目标目录已有反诈插件；请先备份并移出扫描目录。不会覆盖现有文件。' >&2
  exit 1
fi
version='0.2.5-rewrite'
name="fanzha-v${version}-linux-amd64.so"
base="https://github.com/qqlikeyuanshen/cpa-fanzha-ai/releases/download/v${version}"
tmpbase="$(realpath -e -- "${TMPDIR:-/tmp}")"
work="$(mktemp -d "$tmpbase/cpa-fanzha-release.XXXXXX")"
staging=''
cleanup() {
  if [[ -n "${staging:-}" && "$staging" == "$parent"/.fanzha-install.* ]]; then
    rm -f -- "$staging"
  fi
  if [[ -n "${work:-}" && "$work" == "$tmpbase"/cpa-fanzha-release.* && -d "$work" ]]; then
    rm -f -- "$work/$name" "$work/$name.sha256"
    rmdir -- "$work"
  fi
}
trap cleanup EXIT
curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 "$base/$name" -o "$work/$name"
curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 "$base/$name.sha256" -o "$work/$name.sha256"
# Require exactly one checksum line naming only the expected asset.
IFS=' ' read -r expected listed < "$work/$name.sha256"
[[ "$expected" =~ ^[a-fA-F0-9]{64}$ && "$listed" == "$name" ]] || { echo '校验文件格式不正确。' >&2; exit 1; }
actual="$(sha256sum "$work/$name")"
[[ "${actual%% *}" == "${expected,,}" ]] || { echo 'SHA-256 校验失败，未安装。' >&2; exit 1; }
mkdir -p -- "$target"
parent="$(dirname -- "$target")"
staging="$(mktemp "$parent/.fanzha-install.XXXXXX")"
install -m 0644 -- "$work/$name" "$staging"
# Atomically expose the complete file, refusing an existing destination.
ln -T -- "$staging" "$target/$name"
printf '安装完成（Releases 成品）：%s\n' "$target/$name"
printf '未修改配置或重启 CPA。请在 CPA 中启用反诈插件并重新加载。\n'
