# Opt-in interactive late acquisition

The experimental macOS arm64 command can prepare an owned packet and start its
serial source worker before acquiring the drawable. This retains the reviewed
owned-upload/completion patches, fixed shader arrays, GPU-resident geometry and
explicit UVs. The Metal queue retains two credits. Neither the root module nor
shipping renderer/default settings change.

Build an external command and launch explicitly:

```sh
GOWORK=off experiments/webgpu/tools/build-play /tmp/nanolathe-webgpu-play-new --gpu-timing
/tmp/nanolathe-webgpu-play-new/run-play --fast --late-acquire -- --root=/path/to/TotalAnnihilation --map='Town & Country'
```

`--fast` alone preserves early acquisition; the launcher clears inherited
prototype diagnostic flags and preserves the caller's settings selection.
`--late-acquire` requires a reviewed deferred-host build and the owned fast
profile. Normal play needs no diagnostic camera or report. GPU/present telemetry
is optional at runtime even when built. Hardware runtime qualification is limited
to the tested Darwin arm64 Metal path; Windows/Linux checks are compilation only.

All borrowed preparation and copied paint complete before the worker handoff.
The render owner then acquires and validates encoder, view, configured extent and
format before recording any renderer commands. Setup, capture, warmup/final
boundaries, focus changes, long callback gaps and unstable/changed extent use
conservative early acquisition. A recoverable unavailable or changed acquisition
cancels the unencoded surface lease, preserves queue-owned uploads, joins and
drops the pending presentation sample, and requests a fresh early frame. Input
and `Next` are consumed once; no borrowed packet is re-prepared after handoff.
Format migration fails closed. Source time stays anchored across recovery, and fresh input never moves behind
a projected timestamp already consumed by the source worker.

The private GoGPU patch adds render-owner-only deferred-state observation,
pre-command cancellation and an opt-in prohibition on immediate OnDraw replay.
Cancellation cannot inspect arbitrary external encoder commands: the renderer's
pre-command seam is a required precondition. The patch also reconfigures a
same-size restored surface when a zero-size event previously unconfigured it.
The builder verifies the exact original/result source and patch hashes. The
ordinary callback replay policy is retained for callers that do not opt in.

Engine alignment is separate from ordering. The builder's optional
`--engine-source=ARCHIVE --engine-revision=40_HEX_SHA` selects an explicitly
declared root-module archive without merging gameplay into this worktree.
`--presentation-overlay=JSON` accepts exactly the five shared rendering helpers
used in the matched comparison and records their hashes. Default builds use the
existing worktree engine. Benchmark camera/census/extent overlays remain external
qualification artifacts; they are not ordinary play changes.

The bounded native lifecycle gates passed at1920x1080 and2560x1440: three
resize/restore pairs, Hide/Show, two real minimize/restore pairs, real focus
callbacks, two tagged input/focus waves with exactly one receipt and Apply per
event, actual unencoded lease cancellation/recovery, monotonic source time and
close with both source and accepted GPU work outstanding followed by a checked
drain. A separate frozen companion confirmed actual native texture dimensions
joined to successful source/renderer extents through four resize sizes at each
resolution. Missing presentedTime while hidden or reconfigured remains no display
proof; extent values are acquired before Present. Tagged events qualify the input bridge; physical OS keyboard delivery,
relative capture/double-click behavior, external occlusion, display/DPI or format
migration and device loss remain unqualified. QA native helpers/injections are
external overlays and are absent from ordinary play binaries.

Qualification results and remaining limitations are recorded in the local
renderer comparison report. The original comparison, fixed-benchmark A/B/B/A,
steady-frame pixels and native ownership/growth/failure evidence remain retained.
Large scenes do not sustain 120 observed presentations per second. Residual
Metal differences need comparable presentation and workload evidence; unequal
GPU timing envelopes cannot establish exclusive throughput or a per-pass cause.
