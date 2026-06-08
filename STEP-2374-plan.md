# STEP-2374 — V2 inventory layout + schemas

**Status:** Draft for team review
**Jira:** [STEP-2374](https://bitrise.atlassian.net/browse/STEP-2374) — "Define generated file format schemas"
**Confluence:** [Spec.json V2](https://bitrise.atlassian.net/wiki/spaces/~825000090/pages/4923883640/Spec.json+V2)

---

## TL;DR

- Replace today's monolithic 24 MB `spec.json` with a sharded static-file inventory hosted on object storage.
- Two top-level prefixes with a clean architectural boundary:
  - **`steps/`** — source of truth, self-contained, immutable per-version.
  - **`spec/`** — derived index files, regeneratable from `steps/`, short-TTL.
- Each consumer fetches only what it needs (one `step.json` per active step for CI; `latest.json` / `versions.json` for version resolution).
- Estimated per-workflow client bandwidth: **~5.7 MB gzipped (V1)** → **~40 KB (V2)**, a ~140× reduction at the upper bound.
- This document defines the V2 schemas and the scope of PoC A (generator + sample output + size report). PoC B (stepman read path behind a feature flag) is the next step after team sign-off and is scoped separately.

---

## Why (recap)

From the Confluence design doc, three of the four problem statements are addressed here:

1. **`spec.json` is generated locally on each build machine** by `git clone` + walking thousands of `step.yml` files. It's ~24 MB and grows unbounded.
2. **Steplib update is slow at runtime** — adds median ~3.2s to first-step activation. Synthetic benchmarks: 2.3s for a clean steplib update vs 0.48s without.
3. **Single point of failure:** the GitHub steplib repo is the master DB; outage = build failures.

V2 attacks all three: replace the runtime-generated blob with a small set of pre-built, well-cached static files that clients fetch incrementally.

---

## Current state — what stepman does today

Pinned for context; understanding this is necessary to evaluate the V2 design.

1. **Setup** (`stepman.SetupLibrary` in `stepman/library.go`): clones `bitrise-steplib` into `~/.stepman/step_collections/<alias>/collection/`, calls `ReGenerateLibrarySpec`.
2. **Spec generation** (`stepman.WriteStepSpecToFile` via `parseStepCollection` in `stepman/util.go`): walks `collection/steps/**/step.yml`, parses + audits each one, joins per-step `assets/*` URLs against `assets_download_base_uri`, emits `spec/spec.json` (~24 MB) and `spec/slim-spec.json` (latest-version-only, ~2 MB).
3. **Update** (`stepman.UpdateLibrary`): `git pull` + re-run spec generation.
4. **Query** (`stepman.QueryStepInfoFromLibrary` → `ReadStepSpec` → `latestMatchingStepVersion` in `models/version_constraint.go`): unmarshal local `spec.json` into memory, resolve `latest`, `1.x.x`, `1.2.x`, or `1.2.3` against the in-memory hash.
5. **Activate** (`activator/steplib/activate.go`): if `BITRISE_EXPERIMENT_PRECOMPILED_STEPS=true` and `step.Executables[<platform>]` exists, download from `https://storage.googleapis.com/bitrise-steplib-storage`, sha256-verify, copy `step.yml` to destination. Otherwise, fall back to `download_locations` (zip from `bitrise-steplib-collection.s3.amazonaws.com/step-archives/` or git clone).

**Fields stepman actually accesses from step.yml on the hot path:**

| Code site | Field | Purpose |
|---|---|---|
| `activator/steplib_ref.go:49,109` | `Step.Title` | Fall back to step ID in logs |
| `activator/steplib/activate.go:32` | `Step.Executables` | Pick binary for current OS/arch, verify hash |
| `stepman.DownloadStep` (called from `activate_source.go`) | `Step.Source.Commit` | Verify cloned commit |
| `stepman/util.go:266` | `step.AssetURLs` (write) | Generated during spec parse only |

Everything else in `step.yml` is **passed through verbatim** via `copyStepYML` to `current_step.yml`, where bitrise CLI consumes it. So stepman itself reads a tiny subset, but the data it propagates is large.

---

## V2 inventory layout

```
/
├─ meta.json                              ← inventory-level metadata
│
├─ spec/                                  ← DERIVED index files
│  │                                       cache: ETag + short TTL (60s, must-revalidate)
│  ├─ step_ids.json                       ← bare list of step IDs
│  └─ steps/
│     └─ <id>/
│        ├─ latest.json                   ← latest + latest_by_major (resolves Latest/MajorLocked)
│        └─ versions.json                 ← per-step version list (newest-first version strings)
│                                           (resolves MinorLocked + "does this version exist?")
│
└─ steps/                                 ← SOURCE OF TRUTH, self-contained per step
   └─ <id>/
      ├─ step-info.json                   ← maintainer + deprecation + assets (mutable; 5min TTL)
      ├─ assets/                          ← icons / screenshots
      │  └─ icon.svg
      └─ <version>/
         └─ step.json                     ← full per-version step manifest (immutable, 1y TTL)
```

(Prebuilt binaries and source archives are NOT hosted under `steps/`. They
stay in their existing separate storage; `step.json`'s
`executables[*].storage_uri` remains a relative path that the client
resolves against the configured binary storage base, exactly as V1 does
today. Keeping binary storage decoupled from metadata storage is a
deliberate design choice — see follow-up item #3 for the rationale.)

### Architectural invariants

- **`steps/` is the source of truth.** Every file under `spec/` can be deterministically regenerated by walking `steps/`. If `spec/` is corrupt, regenerate; no data is lost.
- **`spec/` is a query-optimized projection.** It exists to spare clients the cost of walking the tree.
- **Immutability is enforced by convention:** once `steps/<id>/<v>/` is published it never changes. Updates are new versions, never edits.

---

## Caching contract

Three CDN/object-storage rules, in priority order. All three major hosting options (Cloudflare, CloudFront, GCP Cloud CDN) express this with prefix or glob rules.

| Pattern | Cache profile | Rationale |
|---|---|---|
| `/spec/*` | `Cache-Control: public, max-age=60, must-revalidate` + `ETag` | Index files change on every release; short revalidation keeps things fresh, ETag avoids unnecessary transfer when unchanged. |
| `/steps/*/step-info.json` | `Cache-Control: public, max-age=300, must-revalidate` + `ETag` | Mutable (deprecation can be added), but rare. 5-min propagation acceptable. |
| `/steps/*` | `Cache-Control: public, max-age=31536000, immutable` | Per-version content is immutable. Cache forever. |

**Cache invalidation:** all three CDNs offer purge-by-URL APIs (Cloudflare Purge, CloudFront `CreateInvalidation`, GCP cache invalidation) for break-glass scenarios. Normal-path correctness is achieved purely via the TTLs above; purge is a safety net.

---

## Schemas

Real values for `git-clone 8.5.0` are used as examples.

### `meta.json`

Inventory-level metadata. Carries the file format version, generation timestamp, and configuration that doesn't belong to any single step.

```json
{
  "format_version": 2,
  "updated_at": "2026-05-15T11:31:34Z",
  "steplib_commit_sha": "b9af7d7abc123def456...",
  "steplib_source": "https://github.com/bitrise-io/bitrise-steplib.git",
  "download_locations": [
    { "type": "zip", "src": "https://bitrise-steplib-collection.s3.amazonaws.com/step-archives/" },
    { "type": "git", "src": "source/git" }
  ]
}
```

| Field | Type | Notes |
|---|---|---|
| `format_version` | int | Major-only schema version. Declared **only** in `meta.json`; per-step and per-version files inherit it transitively (matches V1 / YAML-era convention). Bump only on breaking changes — additive changes don't bump, consumers ignore unknown fields. |
| `updated_at` | ISO 8601 string | When this snapshot was generated. |
| `steplib_commit_sha` | string | Git SHA the generator ran against. Reproducibility + debugging. |
| `steplib_source` | URL | Source repo. Mostly for alt-steplib disambiguation / debugging. |
| `download_locations` | array | Source-archive fallback. **See deferred decision #1** — to be cleaned up after PoC A. |

### `steps/<id>/step-info.json`

Step-level metadata (independent of version). Mirrors today's `step-info.yml` plus the asset list — but as a JSON file. Mutable; 5-min TTL.

```json
{
  "maintainer": "bitrise",
  "deprecation": null,
  "asset_urls": {
    "icon.svg": "assets/icon.svg"
  }
}
```

For a deprecated step:

```json
{
  "maintainer": "community",
  "deprecation": {
    "removal_date": "2025-12-31",
    "notes": "Replaced by `new-step`. See migration guide at …"
  },
  "asset_urls": {
    "icon.svg": "assets/icon.svg"
  }
}
```

| Field | Type | Notes |
|---|---|---|
| `maintainer` | string | `"bitrise"` (verified) or `"community"` or empty. Drives badges. |
| `deprecation` | object \| null | `null` for active steps. Object with `removal_date` (ISO date) and `notes` otherwise. |
| `asset_urls` | map[string]string | **Relative paths** to assets, resolved by the client against the file's own URL. Self-contained. |

### `steps/<id>/<v>/step.json`

The full per-version step manifest. **Field-for-field identical to V1's
`step.yml`**, just JSON-encoded. `models.StepModel` already carries
`json:"…"` tags alongside its `yaml:"…"` tags, so the generator emits the
same shape simply by swapping the marshaler.

```json
{
  "title": "Git Clone Repository",
  "summary": "Checks out the repository, updates submodules and exports git metadata as Step outputs.",
  "description": "The checkout process depends on the Step settings and the build trigger parameters...",

  "website": "https://github.com/bitrise-steplib/steps-git-clone",
  "source_code_url": "https://github.com/bitrise-steplib/steps-git-clone",
  "support_url": "https://github.com/bitrise-steplib/steps-git-clone/issues",

  "published_at": "2026-03-10T12:57:02Z",

  "source": {
    "git": "https://github.com/bitrise-steplib/steps-git-clone.git",
    "commit": "df4081a169df74a8185a653919d223703b2200f6"
  },

  "executables": {
    "darwin-amd64": {
      "storage_uri": "steps/git-clone/8.5.0/bin/git-clone-darwin-amd64",
      "hash": "sha256-9fa46d766238d946e851a2751b61488b422831a45bf1aa81e6afccf272deb841"
    },
    "darwin-arm64": {
      "storage_uri": "steps/git-clone/8.5.0/bin/git-clone-darwin-arm64",
      "hash": "sha256-ee75fc91ef4a4844d48b2f1413b696cc16f4b6167a7e05bf47494088b3abab28"
    },
    "linux-amd64": { "storage_uri": "…", "hash": "sha256-…" },
    "linux-arm64": { "storage_uri": "…", "hash": "sha256-…" }
  },

  "type_tags": ["utility"],

  "toolkit": { "go": { "package_name": "github.com/bitrise-steplib/steps-git-clone" } },

  "is_requires_admin_user": false,
  "is_always_run": false,
  "is_skippable": false,
  "run_if": ".IsCI",

  "inputs": [
    {
      "merge_pr": "yes",
      "opts": {
        "title": "Checkout merged PR state",
        "summary": "Checkout the merged PR state instead of the PR head",
        "description": "This only applies to builds triggered by pull requests...",
        "value_options": ["yes", "no"]
      }
    }
  ],

  "outputs": [
    {
      "GIT_CLONE_COMMIT_HASH": null,
      "opts": {
        "title": "Commit hash",
        "description": "SHA hash of the checked-out commit."
      }
    }
  ]
}
```

**Differences from today's `step.yml`:**

| Change | Why |
|---|---|
| Output is JSON, not YAML | The whole point of V2 — clients fetch + parse incrementally, no YAML dependency required to consume. |

**That's it.** No field renames, no removals, no additions. The generator
takes the parsed `models.StepModel` and emits it via `json.Marshal`. This
means:

- Today's audit / runtime code paths (`activator/`, `cli/`, `toolkits/`,
  etc.) operate on the same `models.StepModel` — only the parser changes
  from `yaml.Unmarshal` to `json.Unmarshal`.
- No V1↔V2 field-name drift to maintain or document.
- The "where should binary URLs come from?" major deferred decision (#3
  below) is genuinely deferred — not pre-empted by an unmotivated rename.

`id` and `version` are deliberately NOT in the file — the file path
`steps/<id>/<version>/step.json` is the canonical identifier, same as
today's `steps/<id>/<version>/step.yml`.

**Sizes (gzipped, measured):**

| Step | step.yml | step.json | gzipped |
|---|---|---|---|
| git-clone 8.5.0 | 12 KB | 12 KB | 3.8 KB |
| xcode-test 6.2.4 | 13 KB | 13 KB | 4.2 KB |
| activate-ssh-key 4.1.1 | 4.3 KB | 4.3 KB | 1.8 KB |
| cache-pull 2.7.2 | 3.4 KB | 3.3 KB | 1.4 KB |
| **Median across 3559 versions** | **~3.4 KB** | **~3.3 KB** | **~1.5 KB** |

### `spec/step_ids.json`

Bare list of valid step IDs. Used to answer "is `<id>` a known step?" without fetching anything else.

```json
{
  "step_ids": [
    "activate-ssh-key",
    "amazon-s3-deploy",
    "android-build",
    "...",
    "git-clone",
    "...",
    "xcode-test"
  ]
}
```

**Size estimate:** ~450 IDs × ~25 chars ≈ 12 KB raw / ~4 KB gzipped.

### `spec/steps/<id>/latest.json`

Per-step latest pointers. Answers `Latest` and `MajorLocked` constraints in one fetch.

```json
{
  "step_id": "git-clone",
  "latest": "8.5.0",
  "latest_by_major": {
    "7": "7.0.3",
    "8": "8.5.0"
  }
}
```

**Size estimate:** ~200–400 bytes raw / ~150–200 bytes gzipped.

### `spec/steps/<id>/versions.json`

Per-step list of version strings, newest-first — what stepman needs for `MinorLocked` resolution (filter `major.minor`, pick highest patch) and the "does this version exist?" check. Only fetched when minor-locked or when the consumer needs the full version list.

```json
{
  "step_id": "git-clone",
  "versions": ["8.5.0", "8.4.2", "8.4.1", "7.0.2"]
}
```

Ordered newest-first, so `versions[0]` is the latest. No `latest` field — the latest pointer lives in `latest.json`. Per-version detail (commit, published_at, executables, ...) is **not** duplicated here either — it lives in each `steps/<id>/<version>/step.json`, fetched once the version is resolved.

**Size estimate:** for git-clone with ~35 versions, well under ~1 KB raw / a few hundred bytes gzipped.

---

## Resolution routes (stepman side)

Stepman recognizes four version-constraint types (see
`models/version_constraint.go`). The V2 layout serves each with the
minimum fetch set:

| Constraint | Example user input | Files fetched | Notes |
|---|---|---|---|
| **Fixed** | `1.2.3` | `steps/<id>/1.2.3/step.json` (1 fetch) | Never touches `spec/`. A 404 is the canonical "no such version" signal. Once fetched, the file is immutable for a year — repeat builds re-validate nothing. |
| **Latest** | `""` / `latest` | `spec/steps/<id>/latest.json` → `steps/<id>/<resolved>/step.json` (2 fetches) | Read `latest` field, then the resolved `step.json`. |
| **MajorLocked** | `1.x.x` or `1` | `spec/steps/<id>/latest.json` → `steps/<id>/<resolved>/step.json` (2 fetches) | **Same file as Latest** — read `latest_by_major["1"]` instead of `latest`. Shared cache key with the Latest route is a real win. |
| **MinorLocked** | `1.2.x` | `spec/steps/<id>/versions.json` → `steps/<id>/<resolved>/step.json` (2 fetches) | Client filters the version list for matching `major.minor`, picks highest patch. `versions.json` is larger (~300 B gz median), so we keep MinorLocked off the small `latest.json`. |

Two design properties this confirms:

1. **The most common production case (Fixed pins) is the cheapest route** — one fetch, immutable, never re-validated. V2's caching wins are biggest for the workflows that need it most.
2. **Latest and MajorLocked share a fetch.** Storing `latest_by_major` alongside `latest` in the same file means a workflow with mixed `latest` + `1.x.x` pins doesn't pay extra round trips.

(Step ID validity — "does step `<id>` exist?" — is implicitly answered by
the 404 / 200 of the resolution fetch itself. Clients that need a
proactive validation list can fetch `spec/step_ids.json` once per
session.)

---

## Per-workflow client bandwidth comparison

10-step workflow, fresh cache, gzipped bytes:

| Variant | Bytes transferred |
|---|---|
| **V1 today** (fetch entire `spec.json`) | ~5,700 KB |
| **V2 — fixed versions only** (10× `step.json`) | ~40 KB |
| **V2 — `latest` resolution** (`step_ids` + 10× `latest.json` + 10× `step.json`) | ~45 KB |
| **V2 — `1.x.x` (major-locked) resolution** | ~45 KB (same — `latest.json` covers this) |
| **V2 — `1.2.x` (minor-locked) resolution** | ~50 KB (adds 10× `versions.json`) |

V2 also benefits from independent CDN cacheability: a workflow re-running with the same fixed-version steps revalidates only the per-step files it touches, instead of re-downloading the 24 MB blob.

---

## PoC A — scope, deliverables, non-goals

### Goal

Produce a runnable Go tool that converts a local clone of `bitrise-steplib` into the V2 inventory tree as defined above, plus documentation and a size report. This lets the team validate the schemas against real data before any stepman runtime code changes.

### Deliverables

1. **`steplibrary/specgen/cmd/steplib-gen/`** — a Go command-line tool in the stepman repo.
   - Input: path to a local clone of `bitrise-steplib` (and an output dir).
   - Walks `steps/**/step.yml` (re-using `stepman.ParseStepDefinition`) and per-step `step-info.yml`.
   - Writes the full V2 tree (all schemas above) to the output directory.
   - Computes `published_at`, `latest_by_major`, `commit`, etc., from the parsed data.
   - Emits a single-line stdout summary per file written; final summary with file count + total bytes.
2. **`docs/spec-v2/`** — schema documentation (essentially the "Schemas" section of this document, plus a JSON Schema file per file type for tooling/IDE validation).
3. **`docs/spec-v2/sample-output/`** — generated V2 tree for a small synthetic steplib (5–10 representative steps including git-clone, activate-ssh-key, xcode-test, cache-pull, and a deprecated step), checked into git for reference.
4. **`docs/spec-v2/report.md`** — comparison report:
   - File counts and total bytes (raw + gzipped) for V2 vs `spec.json` baseline.
   - Per-file-type size distribution.
   - Per-workflow bandwidth simulation (above table reproduced from real numbers).
5. **Tests** — unit tests for the generator covering: a normal step, a deprecated step, a step with multiple platforms in `executables`, a bash step (no executables), and a step with no `step-info.yml`.

### Non-goals for PoC A (explicit)

- No changes to `stepman` runtime code paths.
- No changes to the `bitrise-steplib` repo or its release flow.
- No uploads to any bucket (output is a local directory only).
- No interface refactor inside stepman (a separate engineer is working on the abstraction boundary; we'll integrate after that lands).
- No telemetry instrumentation (relevant only when the read path exists).
- **Binary downloads stay separate from V2 metadata, by design.** Per-version `step.json` carries the V1 `executables[*].storage_uri` relative path verbatim; the client (today's activator) resolves it against the configured binary storage base. We deliberately keep binary storage and metadata storage decoupled so each can scale / move independently.

### Estimated effort

1–2 days for the generator + sample output + report. The schema design is the harder part and it's already done.

---

## Path to PoC B (post-sign-off)

After PoC A is reviewed and the schemas are accepted, PoC B layers on:

1. A new package (e.g., `stepman/inventory/`) with a `Reader` interface — the same operations stepman needs today (`HasStep`, `LatestVersion`, `ResolveVersion`, `ReadStepDefinition`, etc.).
2. A V2 implementation of `Reader` that fetches files over HTTP, with `ETag`-driven revalidation and an on-disk cache.
3. A feature-flag switch (`STEPMAN_USE_V2_INVENTORY=true`) at the entry points in `stepman` / `activator`.
4. Integration tests serving the PoC A output via `httptest.Server` and running real activation flows.

PoC B intersects with another engineer's work on the abstraction boundary; the order of operations and final shape will be coordinated then. C (real GCP bucket end-to-end) is out of scope for now.

---

## Routing strategy: V2 by default, V1 as graceful fallback

How stepman picks between the V2 HTTP reader and the V1 git-clone reader for a given `step_lib_source` URI from the user's `bitrise.yml`. Designed so that:

- GitHub is **not** a hot-path dependency for the V2 flow.
- Default workflows transparently get V2 the moment it's deployed.
- Existing alt-steplib workflows (custom `step_lib_source` URLs) keep working unchanged.
- Alt-steplib operators have a clean opt-in path to V2 later, with no stepman release and no central registry.
- Later `bitrise.yml` can be updated to host the new path and it'll be automatically handled

### Mechanism

The V2 inventory's URL is **compiled into stepman**, with an env-var override for staging / DR. The URI a user types into `bitrise.yml` is just an *identity* (which steplib we're talking about); the V2 URL belongs to stepman itself.

```go
// stepman/inventory/factory.go
const officialV2InventoryURL = "https://steplib-v2.bitrise.io/"
// (precise hosting URL TBD — see Open Questions. Overridable via
//  BITRISE_STEPLIB_V2_INVENTORY_URL for staging / DR.)

// URIs recognized as "the official steplib." Existing user workflows
// pin one of these; matching → routed to the V2 API above.
var officialSteplibURIs = []string{
    "https://github.com/bitrise-io/bitrise-steplib.git",
    // room here for historic redirects, no-.git variants, etc.
}

func NewReader(steplibURI string, log Logger) (Reader, error) {
    if isOfficialSteplib(steplibURI) {
        // Default path: V2 API directly. GitHub is NOT consulted.
        if r, err := newV2Reader(officialV2InventoryURL, log); err == nil {
            return r, nil
        } else {
            // V2 unreachable → graceful fallback to V1 git clone of the
            // official mirror, for THIS build only. Logged + metered.
            log.Warnf("V2 API unreachable (%s); falling back to V1 git clone", err)
            return newV1Reader(steplibURI, log), nil
        }
    }

    // Alt-steplib path. Phase 1: always V1 (today's behavior).
    // Phase 2: see below.
    return newV1Reader(steplibURI, log), nil
}
```

### Routing table

| User's `step_lib_source` in `bitrise.yml` | Stepman route |
|---|---|
| Unset (CLI default = official git URL) | V2 API directly. GitHub untouched on the hot path. |
| Explicitly `https://github.com/bitrise-io/bitrise-steplib.git` | Same — recognized → V2. |
| A custom `*.git` URL (alt-steplib today) | V1 git clone (today's behavior, unchanged). |
| A V2 inventory URL (alt-steplib opting into V2, Phase 2) | V2 reader against their URL. |

### Phase 2: alt-steplib operators on V2

When an alt-steplib operator wants to offer V2 to their users:

1. They generate their own V2 tree with `steplibrary/specgen/cmd/steplib-gen`.
2. They host it on their CDN / bucket / wherever, over HTTPS.
3. Their users change `step_lib_source` in `bitrise.yml` from `https://…/repo.git` to the V2 base URL (e.g., `https://my-cdn.example/steplib/`).

Stepman differentiates a V2 inventory URL from a V1 git URL by URL shape: **a URL ending in `.git` is V1; anything else is treated as a V2 inventory base URL.**

```go
// Phase 2 hook — replaces the alt-steplib branch above:
if strings.HasSuffix(steplibURI, ".git") {
    return newV1Reader(steplibURI, log), nil
}
return newV2Reader(steplibURI, log)
```

If a user has a misconfigured non-`.git` URL pointing at a non-V2-inventory, the V2 reader fails loud on first request. We deliberately **do not** auto-fall-back from a misconfigured custom URL to V1 — silent fallback on user misconfig hides bugs.

No stepman release, no PR to bitrise repos, no central registry is required for an alt-steplib to opt into V2. Each operator owns their own opt-in, declared in their users' `bitrise.yml`.

### Properties this design gives us

| Property | How it falls out |
|---|---|
| GitHub is not on the V2 hot path | Compiled-in V2 URL; no remote probe / no `steplib.yml` fetch over HTTP. |
| Default workflows transparently get V2 | Recognized official URI → V2 reader, no user action. |
| Existing alt-steplib workflows keep working | Unrecognized URI → V1 reader, exactly as today. |
| Alt-steplib operators can adopt V2 independently | Phase 2: change the URL in `bitrise.yml`. No stepman release. |
| Staging / DR controllable | `BITRISE_STEPLIB_V2_INVENTORY_URL` env var overrides the compiled-in URL. |
| Graceful degradation on V2 outage for the official path | Compiled-in mapping knows the corresponding git URL; falls back transparently. |
| Misconfigured custom URLs fail loud | No auto-fallback from alt-steplib V2 → V1; the V2 reader's error is the right signal. |

### Items to nail down before this lands

1. **The actual hosting URL** for the official V2 inventory (Cloudflare Pages vs DC object storage vs GCP, per Confluence Phase 4) — pins `officialV2InventoryURL`.
2. **Fallback policy.** Only on connection-level errors and 5xx, or also on (say) 4xx? My default: connection / 5xx only; 4xx means our deployment is broken and silent fallback would mask it.

---

## Deferred decisions / follow-up action items

Captured in memory so they don't get lost. To be revisited after PoC A is accepted, **before** any production rollout.

### 1. `download_locations` cleanup

Today's `download_locations` shape is opaque: `type: zip` is a URL prefix, `type: git` is the literal string `"source/git"`. The actual zip URL is constructed by stepman from `<prefix>/<id>/<v>/step.zip` — that construction is hardcoded.

For V2 PoC A we roll with the current shape (carried over from `steplib.yml`). **After PoC A**, replace this with something explicit — options:

- Template strings: `"src": "https://…/{id}/{version}/step.zip"` — clients substitute placeholders.
- Fully-resolved URLs in `step.json` (per-version) — kills the global template entirely.
- Drop the zip fallback if it's no longer used in practice (worth checking the metabase).

### 2. Asset URLs (resolved: inventory-root-relative, no `assets_download_base_uri`)

**Decision:** V2 inventory hosts assets directly under `steps/<id>/assets/` (the generator copies them from the source steplib at build time). `step-info.json` emits asset paths relative to its own URL (e.g., `"assets/icon.svg"`); consumers resolve them against the file they fetched them from.

The V1-era `assets_download_base_uri` field (which pointed at the parallel S3 mirror at `https://bitrise-steplib-collection.s3.amazonaws.com/steps`) is **not carried into `meta.json`**. No V2 file references it. The S3 mirror can keep existing for V1 consumer compatibility; V2 simply doesn't depend on it.

Rationale: hard-coding the V1 hosting URL into V2 payloads would lock the V2 inventory to that mirror forever. Relative paths let the V2 inventory be hosted anywhere — staging, mirrors, future migrations — without invalidating the payloads.

### 3. Binary storage (resolved: stays decoupled)

**Decision:** V2 inventory stores metadata only. Prebuilt binaries continue
to live in their existing separate storage (today's GCS bucket via
`BITRISE_PRECOMPILED_STEPS_PRIMARY_STORAGE`), and `step.json` continues to
reference them via the V1 `executables[*].storage_uri` relative path
exactly as `step.yml` does today. No `binary_storage_base_url` in
`meta.json`, no co-location under `steps/<id>/<v>/bin/`, no per-version
absolute URLs.

**Why decoupled, by design:**

- Metadata and binaries have very different change profiles and storage
  needs. Decoupling lets each move / scale / migrate independently
  (e.g., switching binary buckets without regenerating any metadata).
- A binary-bucket incident doesn't take down metadata resolution, and
  vice versa — graceful degradation by construction.
- The existing arrangement already works; nothing forces us to disturb it.

If a future need to migrate the binary base ever arises, the V1
indirection (relative `storage_uri` + client-configured base URL) handles
it without any schema change — same way it would handle it today.

### Smaller deferrals

- **`spec/latest_versions.json` (fat browse catalog)** — **rejected for now.** The single-fetch catalog (one entry per step: title/summary/maintainer/tags/asset_urls/has_executable/…, ~46 KB gzipped) only served browse views (WFE / Integrations Page / `stepman list`); nothing on the stepman resolution path reads it. It also duplicates `step.json`/`step-info.json` data and must be regenerated on every release. We're dropping it to keep the inventory minimal — browse consumers can fetch `step_ids.json` + per-step files, or we re-introduce the catalog later if a browse consumer actually needs the one-fetch shape. Easy to add back: it's a pure projection of `steps/`.
- **`generator_version` in `meta.json`** — good idea, defer until first bug surfaces.
- **Per-use-case JSON splits (CI / WFE / Integrations)** — explicitly rejected after measurement. V2 already captures ~99.3% of the savings; the additional split delivers ~0.5%. Cost of 3× files and a bitrise-CLI consumption audit not worth it.
- **Audit of bitrise-CLI's step.yml consumption** — would be needed if anyone revisits CI-slim. Separate ticket, not part of STEP-2374.

---

## Open questions for the team

1. **Hosting target for the official V2 inventory.** Cloudflare Pages vs DC object storage vs GCP (Confluence Phase 4). Pins the value of `officialV2InventoryURL` in stepman. Not blocking for PoC A; needs alignment before PoC B lands.
2. **Concurrent-release safety.** Today's release flow can't be parallelized. V2 changes this surface; deserves its own design once we get past PoC A. Out of scope here.

(Open question #1 from earlier drafts — "alt-steplib path" — is resolved by the Routing strategy section above.)

---

## Appendix — relevant existing code in stepman

For reviewers wanting to ground the design in the current implementation.

| Concern | Files |
|---|---|
| Spec generation (V1) | `stepman/util.go` (`parseStepCollection`, `WriteStepSpecToFile`, `ReGenerateLibrarySpec`) |
| Library setup / update | `stepman/library.go` |
| Path conventions | `stepman/paths.go` |
| Step model definitions | `models/models.go`, `models/models_methods.go` |
| Version constraint resolution | `models/version_constraint.go` |
| Activation flow | `activator/activator.go`, `activator/steplib_ref.go`, `activator/steplib/activate.go`, `activator/steplib/activate_source.go`, `activator/steplib/activate_executable.go` |
| Benchmarks | (PR #368, mentioned in Confluence) |
