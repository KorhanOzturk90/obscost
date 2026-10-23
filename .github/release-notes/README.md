# Hand-written release notes

A file named after a tag (`v0.1.0.md`) becomes that GitHub Release's notes, in place of the changelog GoReleaser generates from commit messages. `.github/workflows/release.yml` passes it as `--release-notes`. Tags without a file get the generated changelog.

The release footer (container image, checksum verification) is added either way.
