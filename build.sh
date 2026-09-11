#!/bin/bash

set -eu

PROJECT_NAME="cloudflare-tools"
DIST_DIR="releases"

build_frontend() {
    echo "==> 构建前端..."
    cd Frontend
    
    if [ ! -d "node_modules" ]; then
        echo "==> 安装前端依赖..."
        npm ci
    fi
    
    npm run build
    rm -rf ../Server/dist
    cp -r dist ../Server/
    cd ..
}

build_backend() {
    local os=$1
    local arch=$2
    local binary_name="${PROJECT_NAME}"
    local output_name="${PROJECT_NAME}_${os}_${arch}"
    
    echo "==> 构建后端: ${os}/${arch}..."
    cd Server
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags="-s -w" -o "${binary_name}" .

	mkdir -p "../${DIST_DIR}"
	stage_dir="$(mktemp -d)"
	cp "${binary_name}" "${stage_dir}/"
	cp config.yaml.example "${stage_dir}/config.yaml"
	tar -zcvf "../${DIST_DIR}/${output_name}.tar.gz" -C "${stage_dir}" "${binary_name}" config.yaml
	rm -rf "${stage_dir}"
	rm "${binary_name}"
    cd ..
}

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

build_frontend

case "${1:-}" in
    "amd64")
        build_backend "linux" "amd64"
        ;;
    "arm64")
        build_backend "linux" "arm64"
        ;;
    *)
        build_backend "linux" "amd64"
        build_backend "linux" "arm64"
        ;;
esac

echo "==> 构建完成，打包文件位于 ${DIST_DIR} 目录。"
