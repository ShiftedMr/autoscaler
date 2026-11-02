# Google Compute Engine driver

This driver provisions and destroys Compute Engine instances for the autoscaler. It differs from the other drivers in how it copes with stockouts, quota limits and rate limits, because many `Create` calls run concurrently against one shared provider. This document describes that behavior and the settings that shape it.

## Configuration

Setting `DRONE_GOOGLE_PROJECT` selects this driver.

| Variable | Default | Purpose |
|---|---|---|
| `DRONE_GOOGLE_PROJECT` | | Project to create instances in. Required. |
| `DRONE_GOOGLE_ZONE` | `us-central1-a` | Zones to try, comma-separated. See [How `Create` searches](#how-create-searches). |
| `DRONE_GOOGLE_MACHINE_TYPE` | `n1-standard-1` | Primary machine type. |
| `DRONE_GOOGLE_MACHINE_TYPE_ALT` | | Fallback machine types, comma-separated, tried in order after the primary. |
| `DRONE_GOOGLE_MACHINE_IMAGE` | `ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712` | Boot image, as `project/global/images/name`. |
| `DRONE_GOOGLE_DISK_SIZE` | `50` | Boot disk size in GB. |
| `DRONE_GOOGLE_DISK_TYPE` | `pd-standard` | Boot disk type for the primary machine type. |
| `DRONE_GOOGLE_DISK_TYPE_ALT` | | Boot disk type for each fallback machine type, in the same order. If set it must have one entry per fallback machine type. |
| `DRONE_GOOGLE_NETWORK` | `global/networks/default` | Network to attach instances to. |
| `DRONE_GOOGLE_SUBNETWORK` | | Subnetwork to attach instances to. |
| `DRONE_GOOGLE_STACK_TYPE` | `IPV4_ONLY` | IP stack type. `IPV4_IPV6` also requests an external IPv6 address. |
| `DRONE_GOOGLE_PRIVATE_IP` | `false` | When true, instances get no external IPv4 address and the autoscaler connects over the private address. |
| `DRONE_GOOGLE_TAGS` | `allow-docker` | Network tags, comma-separated. With the default tags, the driver creates a `default-allow-docker` firewall rule (tcp 2376) on first use if one does not exist. |
| `DRONE_GOOGLE_LABELS` | | Labels to apply, as comma-separated `key:value` pairs. |
| `DRONE_GOOGLE_SCOPES` | see description | OAuth scopes for the instance's service account, comma-separated. Defaults to `devstorage.read_only`, `logging.write`, `monitoring.write` and `trace.append`, each under `https://www.googleapis.com/auth/`. |
| `DRONE_GOOGLE_SERVICE_ACCOUNT_EMAIL` | `default` | Service account to attach to instances. |
| `DRONE_GOOGLE_USERDATA` | | Cloud-init template, inline. |
| `DRONE_GOOGLE_USERDATA_FILE` | | Path to a cloud-init template. Takes precedence over `DRONE_GOOGLE_USERDATA` if both are set, and must be readable at startup. |
| `DRONE_GOOGLE_USERDATA_KEY` | `user-data` | Metadata key the template is stored under, so it can be set to a startup script instead. |
| `DRONE_GOOGLE_READ_RATELIMIT` | `25` | Requests per second for operation-status reads. |
| `DRONE_GOOGLE_SERVER_CREATE_TIMEOUT` | `3m` | Longest one attempt may take. See [Timeouts](#timeouts). |

The cooldown, retry and destroy timings below are provider defaults and are not exposed as environment variables.

## How `Create` searches

A call works through candidates, where a candidate is one (machine type, zone) pair:

1. Machine types are tried in order: the primary, then each fallback.
2. For each machine type, every zone is tried once, in a random order that is reshuffled per machine type so concurrent calls spread out.
3. The first candidate that yields an instance ends the search.

With no fallback machine types configured, the search is the primary machine type across every zone.

An insert is only treated as finished once Compute Engine reports the operation `DONE`. A failed insert can report its error while the operation is still running, and the instance name stays reserved until it finishes, so acting early would send the next candidate into a name conflict. Inserts carry a request ID so a retried insert is idempotent.

Transient errors (408, 500, 502, 503, 504) are retried with a capped exponential backoff.

## Timeouts

- **Per attempt** (`DRONE_GOOGLE_SERVER_CREATE_TIMEOUT`): bounds one candidate, from the insert through its operation poll and the follow-up read. Time spent waiting for a rate-limit turn is not counted.
- **Whole search**: not configurable. It is derived as the per-attempt timeout plus a fixed allowance for each candidate except the last. This leaves room for one slow attempt after a run of ordinary rejections, so a slow final candidate is not cut off. It also bounds the waiting that happens outside attempts, such as rate-limit turns. A pool with few candidates therefore gets a short budget, so raise `DRONE_GOOGLE_SERVER_CREATE_TIMEOUT` if its rate-limit waits need more room; the budget grows with it.
- **Destroy**: bounded to 15 minutes, covering both the delete request and waiting for the delete operation.

Choose the per-attempt timeout above what a slow-but-successful creation takes. A timeout that fires after Google accepted the insert abandons an instance that may still come up, and that instance is then cleaned up (see below), so too low a value turns working creations into failures.

## Failure handling

Each candidate failure is classified, and the class decides what happens next:

- **Stockout** (zone resource pool exhausted): move on, and put that (zone, machine type) on a short cooldown, 30 seconds by default.
- **Family vCPU quota** (`CPUS_PER_VM_FAMILY`): this quota is scoped to a region and machine family, not a zone. Every zone of that family in the region is put on a cooldown, 5 minutes by default, while other families are unaffected. Other quota errors, such as project-wide CPU quota, are not treated this way.
- **Rate limit**: see below.
- **Name conflict**: the search stops. Every candidate in a call uses the same instance name, so they would all conflict the same way.
- **Anything else** is treated as a likely misconfiguration, such as an invalid machine type or an image the account cannot use. The candidate is skipped and the failure is logged at Error.

Candidates on cooldown are skipped without calling the API. If every zone of a machine type is cooling down, that machine type is skipped.

## Shared coordination

One provider serves every concurrent `Create` and `Destroy`, so it keeps shared state so that calls do not each rediscover the same limit.

**Rate limits** are treated as project- or region-wide, not specific to one candidate. A 429, or a 403 with reason `rateLimitExceeded`, is a rate limit.
- A rate-limit response, from an insert, a delete or an operation poll, records a shared backoff window that honors the server's `Retry-After` (10 seconds if it sends none).
- While a window is active, `Create` calls queue for a turn in FIFO order, and only one attempts at a time.
- A call does not start a wait that would outlast its own deadline. It gives up with a clear "rate limited" error instead of timing out opaquely.
- Consecutive rate-limit hits without an intervening wait, and rate-limit hits while polling one operation, are each capped (5), after which the call gives up on that search or operation.
- `Destroy` publishes its rate-limit hits into the window but is never paced by it.

**Cooldowns** (stockout and family quota) are likewise shared, so one call's discovery protects the others.

## Unconfirmed inserts

Once Google may have accepted an insert, the instance may exist even if the call cannot confirm it. This happens when:
- the insert gets no definitive answer from Google: the request times out or is cancelled, fails at the network level, or keeps returning a transient status (408, 500, 502, 503, 504) through every retry;
- the operation poll gives up, or the per-attempt or whole-search timeout elapses while polling;
- the follow-up read fails or times out.

A definitive answer from Google, such as a stockout, quota error, rate limit or conflict, is not in this group: nothing was created and the search continues. An insert that was never sent, because the context had already expired, is also not in this group. In the cases above `Create`:

- does **not** retry the insert, since the same name would only conflict;
- does **not** delete anything itself;
- returns the instance's identity (ID and zone) together with an error, and stops the search.

The engine records the server as errored along with its ID, and the reaper then destroys it. If nothing was created, the delete returns not-found and is treated as already done. An operation that reaches `DONE` with an error is definitive: nothing was created, no instance is returned, and the search continues.

## Destroy

`Destroy` retries transient errors, treats a not-found response as success, and publishes rate limits to the shared window. Any failure, including a timeout or giving up while waiting on the operation, leaves the server in the errored state for the reaper to retry. A retry after a delete that actually succeeded returns not-found and is treated as done.

## Logging

- Stockouts, quota cooldowns, skipped candidates and rate-limit waits log at Info.
- Slow provider responses log at Warn.
- Unrecognized insert failures, a name conflict, and failed delete operations log at Error. An unrecognized failure logs at Error even if a later candidate succeeds, so a misconfiguration that affects only some candidates is still visible.
- Once the whole search has timed out, the remaining untried candidates are not each logged. The call's single failure is reported by the caller.
