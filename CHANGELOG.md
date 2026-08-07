# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [0.8.0] - 2026-08-07


### Added

- **dataset:** Add the four SMB-shaping dataset fields
- **filesystem:** Add GetACL and SetACL
- **filesystem:** Add PreflightSetPerm for plan-time ACL safety

## [0.7.0] - 2026-08-06


### Added

- **api:** Report the method delta between embedded schemas
- **client:** Add a stateful in-memory fake middleware
- Add SharingNFSService for the sharing.nfs namespace

### Build & CI

- Add release tooling — cliff config, changelog, release task

### Documentation

- Document the release order

### Miscellaneous

- Regenerate FEATURES.md for SharingNFSService

### Testing

- **fake:** Cover the accessors, query filter and verb error paths

## [Unreleased]


### Added

- **api:** Report the method delta between embedded schemas
- **client:** Add a stateful in-memory fake middleware
- Add SharingNFSService for the sharing.nfs namespace

### Miscellaneous

- Regenerate FEATURES.md for SharingNFSService

### Testing

- **fake:** Cover the accessors, query filter and verb error paths

## [0.6.0] - 2026-08-06


### Added

- Add dataset recordsize (#25)
- **api:** Embed an interim 25.10 method schema
- **api:** Resolve the best embedded schema for an appliance version

### Build & CI

- **deps:** Bump golang.org/x/crypto and pin toolchain to go1.25.12
- Enforce lint, staticcheck, govulncheck and a real coverage floor
- **deps:** Enable dependabot for gomod and github-actions

### Changed

- Name the version capability predicates

### Fixed

- **lint:** Resolve pre-existing lint findings

### Miscellaneous

- Create FUNDING.yml
- Add AGENTS.md
- Regenerate FEATURES.md for the 25.10 denominator

## [0.5.0] - 2026-04-02


### Added

- Add webdav support for cloudsync tasks/credentials

## [0.4.2] - 2026-03-22


### Changed

- Run go fmt across all packages

### Fixed

- Make io_type optional when creating/updating disk/raw device

## [0.4.1] - 2026-02-27


### Added

- Add FastList option to CloudSyncTask

### Fixed

- Support three-part TrueNAS version strings (25.10+)

## [0.4.0] - 2026-02-26


### Added

- Add Run method to CronService for cronjob.run API
- Switch CronService.Get to cronjob.get_instance

### Build & CI

- Add Forgejo coverage workflow

### Documentation

- Regenerate feature matrix with CronService.Run

## [0.3.0] - 2026-02-26


### Added

- Add embedded TrueNAS API schema package with version lookup and namespace parsing
- Add feature matrix generator tool
- Add missing API methods for pixels CLI integration
- Add WriteFile to FilesystemService, deprecate on client

### Build & CI

- Add GitHub Actions workflow for Go code coverage reporting
- Remove Forgejo-only restriction from test workflow
- Add GitHub Actions workflow to validate FEATURES.md is up to date
- Add GitHub Actions release workflow with changelog configuration

### Changed

- Split large test files along API namespace boundaries
- Move new tests into established test file layout

### Documentation

- Add feature matrix for TrueNAS 25.04 API coverage and generation tooling

### Fixed

- Rename snapshot test files so featurematrix detects them

### Miscellaneous

- Add 25.04 methods.json
- Ignore .claude

## [0.2.4] - 2026-02-23


### Added

- Add Subscription type, SubscribeCaller interface, and mockSubscribeCaller
- Add Subscribe to Client interface with SSH/Unsupported stubs
- Add SystemService with GetInfo and GetVersion
- Add InterfaceService with List and Get
- Add DockerService with GetStatus and GetConfig
- Add ReportingService with ListGraphs and GetData
- Extend App model with workload, container, and upgrade fields
- Add ReportingService.SubscribeRealtime for real-time metrics
- Add AppService.SubscribeStats and SubscribeContainerLogs
- Add UpgradeSummary, ListImages, AvailableSpace, UpgradeApp, RedeployApp to AppService
- Implement WebSocket Subscribe for collection events
- Support parameterized collection subscriptions in WebSocket client

### Changed

- Replace per-disk map with aggregate disk I/O stats in realtime reporting
- Flatten app stats structure and enrich available versions with human-readable labels

### Documentation

- Add README with usage examples, services overview, and version support
- Add CLAUDE.md with architecture guide and development conventions

## [0.2.3] - 2026-02-23


### Added

- Default to UnsupportedClient when no SSH fallback is configured

## [0.2.2] - 2026-02-22


### Added

- Add Status, Size, Allocated, Free to Pool type
- Add Used, Available to Dataset type

## [0.2.1] - 2026-02-22


### Added

- Export SnapshotServiceAPI interface and MockSnapshotService
- Export CronServiceAPI interface and MockCronService
- Export DatasetServiceAPI interface and MockDatasetService
- Export FilesystemServiceAPI interface and MockFilesystemService
- Export AppServiceAPI interface and MockAppService
- Export CloudSyncServiceAPI interface and MockCloudSyncService
- Export VirtServiceAPI interface and MockVirtService
- Export VMServiceAPI interface and MockVMService

## [0.2.0] - 2026-02-22


### Added

- Add CronService for cronjob.* API namespace
- Add FilesystemService for filesystem API namespace
- Add SnapshotService for snapshot API namespace
- Add CloudSyncService for cloudsync API namespace
- Add DatasetService for pool.dataset and pool.query API namespaces
- Add Compression, Atime, and ForceSize fields to DatasetService
- Add AppService for app and app.registry API namespaces
- Add VirtService for virt instance and device API namespaces
- Add VMService for vm and vm.device API namespaces

### Build & CI

- Add Forgejo CI workflow for running Go tests with gotestfmt

### Changed

- Add composed service interfaces and move WriteFileParams to root
- Extract shared test mocks and add isNotFoundError helper
- Move resolveSnapshotMethod into snapshot_service.go and unexport

### Documentation

- Add CronService design plan

### Fixed

- Add nil-check after re-read in CreateRegistry and UpdateRegistry
- Align VirtService types and error matching with API spec
- Align VMService types with API spec

### Miscellaneous

- Add .worktrees/ to .gitignore
- Clean up docs/plans after CronService complete

## [0.1.0] - 2026-02-21


### Added

- Initial extraction of api package from terraform-provider-truenas
- Add client/ subpackage extracted from terraform-provider-truenas

