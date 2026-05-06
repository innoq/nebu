---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation']
lastStep: 'step-02-generation'
lastSaved: '2026-05-06'
storyId: '9.25'
storyKey: '9-25-sync-gap-buffer-next-batch'
storyFile: '_bmad-output/implementation-artifacts/9-25-sync-gap-buffer-next-batch.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd/atdd-checklist-9-25-sync-gap-buffer-next-batch.md'
generatedTestFiles:
  - 'gateway/internal/matrix/sync_test.go'
inputDocuments:
  - '_bmad-output/implementation-artifacts/9-25-sync-gap-buffer-next-batch.md'
  - 'gateway/internal/matrix/sync.go'
  - 'gateway/internal/matrix/sync_test.go'
  - '_bmad/tea/config.yaml'
redPhaseVerified: true
redPhaseEvidence: |
  make test-unit-go output:
    internal/matrix/sync_test.go:3499:10: undefined: syntheticNextBatch
    internal/matrix/sync_test.go:3500:10: undefined: syntheticNextBatch
  FAIL  github.com/nebu/nebu/internal/matrix [build failed]
---

# ATDD Checklist — Story 9.25: GAP-BUFFER-NEXT-BATCH

## Story

**9.25 GAP-BUFFER-NEXT-BATCH** — Buffer path returns since-token as next_batch  
Story file: `_bmad-output/implementation-artifacts/9-25-sync-gap-buffer-next-batch.md`

## Stack Detection

- Detected stack: `fullstack` (Go backend + Playwright E2E)
- Primary test framework: Go test + httptest
- Test file: `gateway/internal/matrix/sync_test.go`

## Red-Phase Status: CONFIRMED FAILING

All 5 new test functions are in the red phase:

| Test | Red reason | AC |
|---|---|---|
| `TestBuildResponseFromBufferedEvents_NextBatchAdvances` | assertion: `resp.NextBatch == "s42_1"` (echoed token) | AC1 |
| `TestBuildResponseFromBufferedEvents_NextBatchIsMonotonic` | assertion: both tokens identical when using sinceToken echo | AC2 |
| `TestBuildResponseFromBufferedEvents_NextBatchMonotonic_SubMillisecondBurst` | **compile error**: `undefined: syntheticNextBatch` | AC2 edge case |
| `TestHandleIncrementalSync_BufferPath_NextBatchAdvances` | assertion: `next_batch == "s42_1"` (HTTP-level echo) | AC3 |
| `TestHandleIncrementalSync_CorePath_NextBatchUnchanged` | **passes** (non-regression baseline — Core path already correct) | AC4 |

## Acceptance Criteria Coverage

| AC | Test(s) | Coverage |
|---|---|---|
| AC1 — buffer-path NextBatch != sinceToken AND starts with "buf_" | `TestBuildResponseFromBufferedEvents_NextBatchAdvances` | Full |
| AC2 — monotonically increasing (distinct tokens per call) | `TestBuildResponseFromBufferedEvents_NextBatchIsMonotonic` | Full |
| AC2 — sub-millisecond burst (sequence counter) | `TestBuildResponseFromBufferedEvents_NextBatchMonotonic_SubMillisecondBurst` | Full |
| AC3 — HTTP-level: GET /sync?since=s42_1 → next_batch starts with "buf_" | `TestHandleIncrementalSync_BufferPath_NextBatchAdvances` | Full |
| AC4 — non-regression: Core path SinceToken="s99_2" → next_batch == "s99_2" | `TestHandleIncrementalSync_CorePath_NextBatchUnchanged` | Full |

## Implementation Targets (for Dev)

1. Add `var syntheticBatchSeq atomic.Int64` and `func syntheticNextBatch() string` to `sync.go`
2. Replace `NextBatch: sinceToken` with `NextBatch: syntheticNextBatch()` in `buildResponseFromBufferedEvents`
3. Add `"sync/atomic"` to imports in `sync.go`
4. The `sinceToken` parameter may be removed from `buildResponseFromBufferedEvents` (no longer used)
5. Update call sites in `handleIncrementalSync` (~line 539, 553)

## Oracle Compliance Notes

- next_batch MUST advance monotonically — never echo back the since= token (Matrix spec)
- Synthetic token format: `buf_<ms>_<seq>` — distinct across sequential calls
- Core/Elixir path token (`deltaResp.GetSinceToken()`) MUST pass through unchanged
- Synthetic token sent as since= on next poll → FallbackToInitial (safe — already tested)
