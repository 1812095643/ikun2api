#!/usr/bin/env bash
set -euo pipefail
: "${VERSION:?}" "${COMMIT:?}" "${BUILD_DATE:?}"
root=$(pwd)
output="$root/dist/release"
mkdir -p "$output"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${target%/*}; arch=${target#*/}; ext=''
  [[ "$os" != windows ]] || ext='.exe'
  stage=$(mktemp -d)
  trap 'rm -rf "$stage"' EXIT
  (cd backend && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -tags embed -trimpath \
    -ldflags="-s -w -X main.Version=$VERSION -X main.Commit=$COMMIT -X main.Date=$BUILD_DATE -X main.BuildType=release" \
    -o "$stage/ikun2api$ext" ./cmd/server)
  mkdir -p "$stage/deploy"
  for file in deploy/*.sh deploy/*.md deploy/*.service deploy/docker-compose*.yml deploy/config.example.yaml deploy/.env.example; do
    [[ ! -f "$file" ]] || cp "$file" "$stage/deploy/"
  done
  cp -r backend/resources "$stage/resources"
  cp LICENSE* COPYING.GPL NOTICE.md README* "$stage/"
  for name in ikun2api; do
    archive="${name}_${VERSION}_${os}_${arch}"
    if [[ "$os" == windows ]]; then
      python3 - "$stage" "$output/$archive.zip" <<'PYZIP'
import pathlib, sys, zipfile
root = pathlib.Path(sys.argv[1])
with zipfile.ZipFile(sys.argv[2], 'w', zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(root.rglob('*')):
        if path.is_file():
            archive.write(path, path.relative_to(root))
PYZIP
    else
      tar -czf "$output/$archive.tar.gz" -C "$stage" .
    fi
  done
  rm -rf "$stage"
  trap - EXIT
done
(cd "$output" && sha256sum ./*.tar.gz ./*.zip | sed 's|  ./|  |' > checksums.txt)
