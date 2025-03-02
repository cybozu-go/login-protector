# Release procedure

This document describes how to release a new version.

## Versioning

Follow [semantic versioning 2.0.0](https://semver.org/spec/v2.0.0.html) to choose the new version number.

## Prepare change log entries

Before you release, label pull requests appropriately. The release workflow creates a release note referring to the labels on the pull requests. You can check the rules for labeling in [release.yml](/.github/release.yml).

## Run release workflow

Run release workflow on [Actions tab](https://github.com/cybozu-go/login-protector/actions/workflows/release.yaml) with the release version `X.Y.Z`.
