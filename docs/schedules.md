# Scheduled (recurring) jobs

Hopper has a built-in cron scheduler. Register a schedule once and the control
plane enqueues the job on your cron expression — no external cron, no babysitting.

## Create one

CLI:

```bash
# Run a backup container every night at 02:30
hopper schedule create --name nightly-backup --cron "30 2 * * *" \
  --image myorg/backup:latest --cmd "/backup.sh"

hopper schedule list
hopper schedule rm <schedule-id>
```

API:

```bash
curl -X POST $HOPPER_CONTROL_URL/api/schedules \
  -H "Authorization: Bearer $HOPPER_OPERATOR_TOKEN" \
  -d '{"name":"nightly","cron":"30 2 * * *","spec":{"image":"myorg/backup:latest","command":["/backup.sh"]}}'
```

## Cron syntax

Standard 5 fields: `minute hour day-of-month month day-of-week`.

| Field | Range |
|-------|-------|
| minute | 0–59 |
| hour | 0–23 |
| day of month | 1–31 |
| month | 1–12 |
| day of week | 0–6 (0 = Sunday; 7 also = Sunday) |

Each field supports `*`, lists (`1,15`), ranges (`9-17`), and steps (`*/15`, `9-17/2`). When
both day-of-month and day-of-week are restricted, a time matches if it matches *either* (the
usual cron behaviour).

Examples:

| Expression | Meaning |
|------------|---------|
| `* * * * *` | every minute |
| `0 * * * *` | top of every hour |
| `*/15 9-17 * * 1-5` | every 15 min, 9am–5pm, weekdays |
| `30 2 * * *` | 02:30 every day |
| `0 0 * * 0` | midnight every Sunday |

## How it works

The scheduler ticks every 30s, finds schedules whose next run is due, submits a job from the
stored spec (tagged `submitted_by: schedule:<id>`), and computes the next run. Times are
**UTC**. A scheduled job is a normal job — it respects labels, priority, retries, artifacts,
and shows up in the console and `hopper jobs` like any other.
