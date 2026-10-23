# Packaging-only image: GoReleaser (dockers_v2 in .goreleaser.yaml) builds
# the binaries and hands buildx a context laid out as
# <os>/<arch>/<binary>, so nothing is compiled here. To build it by hand,
# use `make release-snapshot`, not `docker build .`.
#
# distroless/static: CA certificates and /etc/passwd, no shell, no libc.
# Pinned by digest (a multi-arch index) for reproducible images; the
# :nonroot tag runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG TARGETPLATFORM
# The binary's name, from project_name in .goreleaser.yaml. It is copied to
# a fixed path so a rename needs no change here.
ARG BINARY

COPY ${TARGETPLATFORM}/${BINARY} /usr/local/bin/app

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/app"]
