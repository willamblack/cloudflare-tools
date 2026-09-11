FROM node:20-bookworm-slim AS frontend-builder

WORKDIR /app/frontend

COPY Frontend/package*.json ./
RUN npm ci

COPY Frontend/ ./
RUN npm run build

FROM golang:1.25.14-bookworm AS backend-builder

WORKDIR /app

COPY Server/go.mod Server/go.sum ./
RUN go mod download

COPY Server/ ./
COPY --from=frontend-builder /app/frontend/dist ./dist

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o cloudflare-tools .

FROM debian:bookworm-slim

ARG ACME_SH_VERSION=3.1.4
ARG ACME_SH_SHA256=e5f8e187bbf5251e0cd8891f2622daab9850366bd17bea9f92c2fe2ee091fd32

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
    socat \
    openssl \
	&& curl -fL --retry 3 --proto '=https' --tlsv1.2 \
		-o /tmp/acme.sh.tar.gz "https://github.com/acmesh-official/acme.sh/archive/refs/tags/${ACME_SH_VERSION}.tar.gz" \
	&& echo "${ACME_SH_SHA256}  /tmp/acme.sh.tar.gz" | sha256sum -c - \
	&& tar -xzf /tmp/acme.sh.tar.gz -C /tmp \
	&& (cd "/tmp/acme.sh-${ACME_SH_VERSION}" \
		&& ./acme.sh --install --home /root/.acme.sh --config-home /root/.acme.sh --nocron) \
	&& rm -rf /tmp/acme.sh.tar.gz "/tmp/acme.sh-${ACME_SH_VERSION}" /var/lib/apt/lists/*

WORKDIR /app

COPY --from=backend-builder /app/cloudflare-tools /app/cloudflare-tools
COPY LICENSE /usr/share/licenses/cloudflare-tools/LICENSE

ENV PATH="/root/.acme.sh:${PATH}"
ENV DATA_DIR=/data

WORKDIR /data

RUN mkdir -p /data/certs && chmod 0700 /data /data/certs

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/healthz || exit 1

CMD ["/app/cloudflare-tools"]
