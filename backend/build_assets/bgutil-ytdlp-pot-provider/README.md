# Pinned yt-dlp POT provider build input

- Upstream: [Brainicism/bgutil-ytdlp-pot-provider, tag 1.3.1](https://github.com/Brainicism/bgutil-ytdlp-pot-provider/tree/1.3.1).
- Release asset: [bgutil-ytdlp-pot-provider.zip](https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/download/1.3.1/bgutil-ytdlp-pot-provider.zip), stored here with the version in its filename.
- Archive SHA-256: `b8ceec7f76143da172aaf5ebeec0c2d218e5680c063b931586bca48567069b38`.

The ZIP is the unchanged archive recovered from the already deployed API image.
Its digest matches the existing Dockerfile pin. It contains the three upstream
Python source modules under `yt_dlp_plugins/extractor/`; it is not a repackaged
or compiled substitute.

`LICENSE` is the verbatim [upstream GPLv3 license at tag 1.3.1](https://github.com/Brainicism/bgutil-ytdlp-pot-provider/blob/1.3.1/LICENSE)
(SHA-256 `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986`).
The API image keeps this license beside the source ZIP in the yt-dlp plugin
directory.

The Dockerfile copies the selected local version and verifies its digest. A
missing version or mismatched digest fails the build; there is no download
fallback. This removes the plugin release download from builds, while other
build dependencies retain their existing installation procedures.

To update deliberately:

1. Obtain the chosen upstream release ZIP and the license from the same tag.
   Verify their provenance and record the archive SHA-256 before adding them.
2. Store the unmodified ZIP as `bgutil-ytdlp-pot-provider-<version>.zip`, preserve
   its complete source, and retain the upstream license verbatim.
3. Update both Dockerfile pins, the deployment tests, and this provenance note.
   Keep the Compose provider service version compatible with the plugin.
4. Run `go test ./internal/youtuberelay` from `backend`, then verify the Docker
   installation offline and confirm corrupt or absent inputs fail.

An explicit `BGUTIL_PROVIDER_VERSION` override requires a corresponding vendored
archive and the matching `BGUTIL_PROVIDER_SHA256`; it never triggers a fetch.
