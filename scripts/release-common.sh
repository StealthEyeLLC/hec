#!/usr/bin/env bash

hec_release_name_is_valid() {
  local name=${1:-}
  [[ -n $name ]] || return 1
  [[ $name =~ ^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$ ]] || return 1
  [[ $name != *..* ]] || return 1
}

hec_require_release_name() {
  local name=${1:-}
  if ! hec_release_name_is_valid "$name"; then
    printf 'invalid HEC release version: %q\n' "$name" >&2
    return 1
  fi
}

hec_full_commit_is_valid() {
  [[ ${1:-} =~ ^[0-9a-f]{40}$|^[0-9a-f]{64}$ ]]
}

hec_release_trees_identical() {
  local left=$1
  local right=$2
  python3 - "$left" "$right" <<'PY'
import os
import pathlib
import stat
import sys

left = pathlib.Path(sys.argv[1])
right = pathlib.Path(sys.argv[2])


def entries(root: pathlib.Path):
    result = {}
    for path in [root, *sorted(root.rglob("*"))]:
        rel = path.relative_to(root)
        info = path.lstat()
        mode = stat.S_IMODE(info.st_mode)
        if stat.S_ISREG(info.st_mode):
            kind = "file"
            payload = path.read_bytes()
        elif stat.S_ISDIR(info.st_mode):
            kind = "dir"
            payload = None
        elif stat.S_ISLNK(info.st_mode):
            kind = "symlink"
            payload = os.readlink(path)
        else:
            kind = f"mode:{stat.S_IFMT(info.st_mode)}"
            payload = None
        result[str(rel)] = (kind, mode, info.st_uid, info.st_gid, payload)
    return result

raise SystemExit(0 if entries(left) == entries(right) else 1)
PY
}

hec_install_staged_release() {
  local staging=$1
  local final=$2

  if [[ -e $final || -L $final ]]; then
    if [[ ! -d $final || -L $final ]]; then
      echo "release path exists but is not a real directory: $final" >&2
      return 1
    fi
    if hec_release_trees_identical "$staging" "$final"; then
      return 0
    fi
    echo "release $final already exists with different content" >&2
    return 1
  fi

  mv -- "$staging" "$final"
}
