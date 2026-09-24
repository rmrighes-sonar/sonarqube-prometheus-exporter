# No compilation happens here. The Go binary is cross-compiled once, on the
# GitHub Actions runner, in ci.yml's `build` job (Go's native cross-compiler
# targets linux/amd64 and linux/arm64 directly -- no QEMU/emulation needed)
# and handed to this stage as a build-context artifact under
# dist/linux/<arch>/sonarqube-prometheus-exporter. This stage's only job is
# to assemble the minimal runtime image around that prebuilt binary, so
# `docker buildx build --platform linux/amd64,linux/arm64` never recompiles
# anything -- it just copies the right file per target platform.
#
# Building locally? See the "Building locally" section of README.md for the
# matching `go build` invocation that populates dist/ before this will work.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget && \
    adduser -D -H -u 10001 exporter
# TARGETARCH is set automatically by BuildKit/buildx per target platform
# (e.g. "amd64", "arm64") -- no need to pass it explicitly.
ARG TARGETARCH
# --chmod=755 is required, not just nice-to-have: actions/upload-artifact
# and actions/download-artifact (ci.yml's build -> publish artifact
# handoff) don't reliably preserve Unix executable permission bits across
# their zip round-trip, so the binary can arrive here without +x even
# though it left `go build` with it. Without this, the container fails at
# runtime with "permission denied" trying to exec the entrypoint --
# confirmed by an actual publish+pull of this image.
COPY --chmod=755 dist/linux/${TARGETARCH}/sonarqube-prometheus-exporter /usr/local/bin/sonarqube-prometheus-exporter
USER exporter
EXPOSE 9091
ENTRYPOINT ["/usr/local/bin/sonarqube-prometheus-exporter"]
