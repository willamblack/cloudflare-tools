#!/bin/sh

set -eu

ACME_SH_VERSION="3.1.4"
ACME_SH_SHA256="e5f8e187bbf5251e0cd8891f2622daab9850366bd17bea9f92c2fe2ee091fd32"

echo "正在安装 acme.sh..."

if [ -d "$HOME/.acme.sh" ]; then
    echo "acme.sh 已安装，跳过安装步骤"
    exit 0
fi

TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT
ARCHIVE="$TEMP_DIR/acme.sh.tar.gz"

curl -fL --retry 3 --proto '=https' --tlsv1.2 \
    -o "$ARCHIVE" "https://github.com/acmesh-official/acme.sh/archive/refs/tags/${ACME_SH_VERSION}.tar.gz"

if command -v sha256sum >/dev/null 2>&1; then
    echo "$ACME_SH_SHA256  $ARCHIVE" | sha256sum -c -
else
    actual_sha256="$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')"
    test "$actual_sha256" = "$ACME_SH_SHA256"
fi

tar -xzf "$ARCHIVE" -C "$TEMP_DIR"
(cd "$TEMP_DIR/acme.sh-${ACME_SH_VERSION}" && ./acme.sh --install --home "$HOME/.acme.sh" --config-home "$HOME/.acme.sh")

echo "acme.sh ${ACME_SH_VERSION} 安装成功。"
echo "请重新加载 shell，或将 $HOME/.acme.sh 加入 PATH。"
