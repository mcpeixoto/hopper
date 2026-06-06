# Completion webhooks

Hopper can POST a JSON event to a URL you choose whenever a job finishes — wire it to a
dashboard, an automation, or a Slack/Discord relay.

## Enable

```bash
HOPPER_NOTIFY_URL=https://example.com/hooks/hopper
HOPPER_NOTIFY_SECRET=$(openssl rand -hex 32)   # optional but recommended
```

## Payload

On every terminal transition (`done`, `failed`, `cancelled`) Hopper POSTs:

```json
{
  "event": "job.failed",
  "job_id": "job_6327ae34180f",
  "status": "failed",
  "image": "alpine:3.20",
  "exit_code": 1,
  "error": "non-zero exit",
  "attempts": 3,
  "submitted_by": "schedule:sch_abc",
  "finished_at": "2026-06-07T08:00:00Z"
}
```

A requeued failure (attempts remaining) is **not** a terminal transition and does not fire —
you only get notified when a job is truly done, failed for good, or cancelled.

## Verifying authenticity

If `HOPPER_NOTIFY_SECRET` is set, each request carries
`X-Hopper-Signature-256: sha256=<hmac>` — the HMAC-SHA256 of the raw body with your secret.
Verify it the same way you'd verify a GitHub webhook:

```python
import hmac, hashlib
expected = "sha256=" + hmac.new(secret, body, hashlib.sha256).hexdigest()
assert hmac.compare_digest(expected, request.headers["X-Hopper-Signature-256"])
```

Delivery is best-effort and asynchronous (a 10s timeout, no retries) so a slow or failing
receiver never blocks job completion.
