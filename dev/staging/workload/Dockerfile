# loadgen image for the staging cluster. `make image` builds the static
# binary on the host first (so the build needs no network inside Docker,
# which matters behind a proxy) and copies it in.
FROM alpine:3.22
COPY .run/loadgen-linux-amd64 /usr/local/bin/loadgen
COPY profiles /etc/loadgen/profiles
USER 65534
ENTRYPOINT ["/usr/local/bin/loadgen"]
