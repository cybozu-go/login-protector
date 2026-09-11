# Release procedure

This document describes how to release a new version.

## Versioning

Follow [semantic versioning 2.0.0](https://semver.org/spec/v2.0.0.html) to choose the new version number.

## Prepare change log entries

Before you release, label pull requests appropriately. The release workflow creates a release note referring to the labels on the pull requests. You can check the rules for labeling in [release.yml](/.github/release.yml).

## Create a release pull request

1. Update the [`VERSION`](/VERSION) file to the new version number `X.Y.Z`.
2. Regenerate the installer manifest:

   ```sh
   PROTECTOR_IMG=ghcr.io/cybozu-go/login-protector:X.Y.Z make build-installer
   ```

3. Commit the updated `VERSION` and `dist/install.yaml`, and open a pull request against `main`.
4. Get the pull request reviewed and merge it.

## Release

Once the pull request is merged, the [Release workflow](https://github.com/cybozu-go/login-protector/actions/workflows/release.yaml) runs automatically on `main`, detects that `vX.Y.Z` has not been released yet, and builds and pushes the container images, pushes the `vX.Y.Z` tag, and creates the GitHub release with `dist/install.yaml` attached.
