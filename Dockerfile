FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download && go mod verify
COPY . .
ARG COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w -X github.com/DreamDonghao/pageweave/internal/buildinfo.Commit=${COMMIT}" -o /out/pageweave ./cmd/pageweave
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go test -mod=readonly -tags=browser -c -o /out/browser.test ./internal/extraction

FROM debian:12.12-slim@sha256:d5d3f9c23164ea16f31852f95bd5959aad1c5e854332fe00f7b3a20fcc9f635c AS runtime
ARG APT_MIRROR=http://deb.debian.org
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    sed -i "s|http://deb.debian.org|${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources \
    && rm -f /etc/apt/apt.conf.d/docker-clean && apt-get -o Acquire::Retries=3 update \
    && apt-get -o Acquire::Retries=3 install -y --no-install-recommends chromium ca-certificates fonts-noto-cjk \
    && useradd --uid 10001 --create-home --shell /usr/sbin/nologin pageweave \
    && chromium --version > /etc/pageweave-chromium-version
COPY --from=builder /out/pageweave /usr/local/bin/pageweave
LABEL org.opencontainers.image.title="PageWeave" org.opencontainers.image.licenses="AGPL-3.0-only"
ENV PAGEWEAVE_BROWSER_PATH=/usr/bin/chromium
USER 10001:10001
WORKDIR /home/pageweave
EXPOSE 7779
HEALTHCHECK --interval=15s --timeout=3s --start-period=20s --retries=3 CMD ["pageweave", "healthcheck"]
ENTRYPOINT ["pageweave"]

FROM runtime AS browser-test
COPY --from=builder /out/browser.test /usr/local/bin/browser.test
COPY internal/extraction/testdata /home/pageweave/testdata
ENTRYPOINT ["/usr/local/bin/browser.test"]
CMD ["-test.v", "-test.timeout=180s"]

FROM runtime AS final
