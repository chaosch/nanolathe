# Dependency diagnostics, not an adopted fork

`bindgroup-v29-diagnostic.patch` applies to `go-webgpu/webgpu v0.5.5`
`wgpu/bindgroup.go`, licensed MIT, Copyright 2025 Andrey Kolkov and the
Go-webgpu Authors. This experiment-authored patch inserts the default-zero
binding-array field from wgpu-native v29's public header. It was applied to a
copy in `/private/tmp/nanolathe-webgpu-20261010/webgpu-patched`, using a temporary
modfile replace. The module cache and committed dependency graph stay unchanged.

Regression case: the P0 smoke creates a storage-buffer layout with a 16-byte
minimum and an RGBA texture/sampler layout. Unpatched, it aborts in layout
creation. Patched, it reaches compute dispatch, render and buffer mapping, then
aborts in native GetMapState. The full logs are outside the repo. This patch has
**not** passed independent review or the complete ABI suite; it is evidence for
a stopped dependency investigation and must not be used as an adoption fix.

`native-artifacts.json` pins release archive digests from the upstream release
API. Only macOS arm64 was downloaded and its archive and dylib hashed locally;
Windows/Linux digests are publisher metadata, not locally verified binaries.
GoGPU, wgpu, gpucontext, gputypes, naga, goffi and go-webgpu at the resolved pins
carry MIT licenses. x/sys carries BSD-3-Clause. wgpu-native is MIT/Apache-2.0;
redistribution must preserve its source/license notices and its Rust transitive
notices. Packaging a redistributable Rust bundle is unqualified while runtime
fails. No GPL code was imported.

The P4 public Metal comparison imports the root's existing purego v0.11.1
(Apache-2.0) into this nested module only. Its downloaded LICENSE was audited;
no source is copied and the root module graph is unchanged. A distributed
comparison binary needs its license notice.


`metal-pointercache-diagnostic.patch` is a separate exact-wgpu-v0.34.2
macOS arm64 experiment. Modified upstream ObjC source is MIT, Copyright2025
The GoGPU Authors; added cache/test code is independently authored MIT,
Copyright2026 The Nanolathe Authors. Private copies preserve upstream LICENSE.
Prepared pointer signatures share immutable metadata, while each call owns its
pooled CIF, argument values and zeroed result storage. Mixed/aggregate/float
calls are unchanged; unsupported argument counts retain the original error.

Reproduce with `tools/build-play NEW_EXTERNAL_DIR --metal-cache`; the helper
checks the exact existing module pin and copies it outside the repository.
Test the copy using `CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run
TestPointerCache ./hal/metal`; concurrency/recursive GC/register-stack/return
cases are included. The race suite uses `CGO_ENABLED=1 go test -race -tags
nofakecgo -run TestPointerCache ./hal/metal` for testing only. All published
prototype builds remain pureGo/CGO0. Other architectures, full loss/shutdown
failure paths and adoption remain unqualified. See FOLLOWUP_REPORT.md for
microbenchmark and whole-game allocation/timing/pixel evidence.
