Hardcode the assumption that if default_step_lib_source == OFFICIAL → has web API, no need to git clone
-> later

Feature flagging (env var) -> done

Refactors to abstract spec.json writes, reads behind an interface → have 2 implementations for all steplib-related operations

Implement web API fetching for each operation

Caching of responses: solve it on the HTTP layer with Cache-Control headers and an on-disk/in-memory cache

locking for safe parallel in-process (and multi-process support) usage from the start. Hard to retrofit otherwise.

## Spec v2 format


steps <---- contents is git steplib repo + step binaries + src.zips. It is ‘immutable’ as in we only add files here. can be cached aggressively

|- < step_id >

    |- step-info.yml

    |- assets

    |- <step_version>

       |- step.yml

       |- bin <----- place prebuilt binaries here

        |- src.zip ← step source zip (for bash steps, not precompiled go steps + compatibility)

spec <---- updated dynamically. Etag + short cache

|- step_ids.json. <----- all step ids

|- step_latest_versions.json <----- all latest versions (latest, latest minor, latest major)

|- all_step_versions.json <------ all step versions (+ metadata?)

|- updated_timestamp  OR commit_hash<--- for sanity check/reference of which version from git 


