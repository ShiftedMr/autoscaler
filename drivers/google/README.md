# Google Compute Engine driver

This driver provisions and destroys Compute Engine instances for the autoscaler. This document describes how it handles slow, failed and rate-limited calls, and the settings that shape it.

## Configuration

| Variable | Purpose |
|---|---|
| `DRONE_GOOGLE_SERVER_CREATE_TIMEOUT` | Longest a `Create` call may take (default `3m`). See [Timeouts](#timeouts). |
| `DRONE_GOOGLE_READ_RATELIMIT` | Requests per second for operation-status reads (default `25`). |

The destroy timeout and retry timings below are provider defaults and are not exposed as environment variables.

## Operations

An insert or delete is only treated as finished once Compute Engine reports the operation `DONE`. A failed operation can report its error while it is still running, and a failed insert keeps its instance name reserved until the operation finishes, so acting on the error early could re-insert the same name and hit a conflict. Inserts carry a request ID so a retried insert is idempotent.

An operation that reaches `DONE` with an error is definitive: nothing was created.

Transient errors (408, 500, 502, 503, 504) are retried with a capped exponential backoff. A rate limit, either a 429 or a 403 with reason `rateLimitExceeded`, is retried after the server's `Retry-After` (10 seconds if it sends none).

## Timeouts

- **Create** (`DRONE_GOOGLE_SERVER_CREATE_TIMEOUT`): bounds the whole call, from the insert through its operation poll and the follow-up read.
- **Destroy**: bounded to 15 minutes, covering both the delete request and waiting for the delete operation.

Choose the create timeout above what a slow-but-successful creation takes. A timeout that fires after Google accepted the insert leaves an instance that may still come up, and that instance is then cleaned up (see below), so too low a value turns working creations into failures.

## Unconfirmed inserts

Once Google has accepted an insert, the instance may exist even if `Create` cannot confirm it. This happens when the operation poll gives up, the timeout elapses, or the follow-up read fails. In those cases `Create`:

- does **not** retry the insert, since the same name would only conflict;
- does **not** delete anything itself;
- returns the instance's identity (ID and zone) together with an error.

The engine records the server as errored along with its ID, and the reaper then destroys it. If nothing was created, the delete returns not-found and is treated as already done. Without this, an errored server has no ID, so the reaper removes its database row and leaves the running VM behind.

## Destroy

`Destroy` retries transient errors and treats a not-found response as success. Any other failure, including a timeout while waiting on the operation, leaves the server in the errored state for the reaper to retry. A retry after a delete that actually succeeded returns not-found and is treated as done.

## Logging

- A slow response, where the create timeout elapsed before the insert finished, logs at Warn.
- A failed insert operation, a failed read-back, and a failed delete operation log at Error.
- A delete that was accepted but could not be confirmed logs at Warn.
