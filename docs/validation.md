# Demand validation — the falsification gate

The product plan puts a falsification gate in front of the product. This file is its
record. It is written to be checkable, so it separates what can be cited today
from what still needs a human.

## What the gate requires

The plan's first milestone, the demand gate:

> Weekend-1 falsification: cite 2+ real agent collision/circular-wait incidents
> and run 5 developer interviews; `docs/validation.md` carries the go/no-go — no
> product code until it passes

and its pass condition:

> Fewer than 2 cited incidents, or fewer than 2 of 5 interviews reporting lost
> work → kill, write no m2.

The gate has two independent halves: **cited incidents** and **developer
interviews**. Both are counted below.

## Half one — cited incidents: 0 of 2 required

The plan's own evidence base is topic-level. Its three sources are recorded here
with what each does and does not establish. **None of them reports an actual
collision or circular wait**, so the incident count is zero.

| Source | What it says | Does it cite an incident? |
| --- | --- | --- |
| [r/LocalLLaMA — "24gb is plenty if your engines take turns"](https://www.reddit.com/r/LocalLLaMA/comments/1xahi82/24gb_is_plenty_if_your_engines_take_turns/) | *"Don't have to swap anything manually, collisions avoided, VRAM fully dedicated to each function while it's running (no VRAM squatters)."* | **No.** It is about swapping one GPU between engines by hand. The word "collisions" describes what manual serialisation avoids, not an incident that happened. Engagement was not captured (null upvotes and comments). |
| [Hacker News — `fixture-inc/swarmatory`](https://github.com/fixture-inc/swarmatory) | "Shared chatroom/feed for your team's coding agents" — 21 points, 0 comments. | **No.** A chatroom/feed is an observability surface. 0 comments means nobody reported pain in the thread. |
| [GitHub trending — `tinyhumansai/openhuman`](https://github.com/tinyhumansai/openhuman) | "Run more than 500 agents on a $10 VPS" — 42,354 stars. | **No.** Popularity of a harness, not a user's collision. |

The plan's own analysis report reaches the same conclusion, in its words: *"Three
sources, all of them topic-level. None of them cites an actual collision or
circular-wait incident."* Its listed falsifier is exactly this count: *"Cite 2+
users hitting a real agent collision or circular-wait incident. If a search for
real collision or circular-wait reports … yields fewer than two, `demand_real`
stays false and the case remains unproven."*

**Incident count today: 0.** Two are required. This half does not pass.

## Half two — developer interviews: 0 of 5, pending human action

No interviews have been run. This is a human research step and it is **not**
simulated or back-filled here.

### The interview to run

Ask five developers who run two or more coding agents against one repository,
GPU or API quota. One question carries the gate:

> Have you lost work to two agents contending for one resource — a file, a port,
> a GPU, or an API quota — and what did you do about it?

Follow-ups that separate awareness from pain:

1. What exactly was lost — time, a commit, a run?
2. What did you do instead: serialise, shard, worktrees, a manual lock, nothing?
3. Did the two agents wait on each other, or did one simply overwrite the other?
4. How often has it happened in the last month?
5. Would a wrapper that refuses the circular wait before it starts have changed
   the outcome?

Record each answer as: who, the resource, what was lost, and whether they
already work around it. The plan's absorption and workload kill criteria both
turn on answers 2 and 4.

### Where to record it

Append to this file under a new `## Interview log` heading, one entry per
developer, with the date and the answer to the gate question. Count only
developers who report **lost work**, not developers who recognise the problem.

## Verdict today: NO-GO on the demand gate

Both halves are unmet — 0 of 2 incidents, 0 of 5 interviews. Read strictly, the
plan's pass condition is not satisfied, and its kill criteria say not to build
the broker.

That verdict is recorded rather than argued away, and it has a consequence worth
stating plainly:

- **Nothing in this repo claims validated demand.** The README describes what
  the tool does and what it does not do. It does not claim that developers are
  losing work today, because that is exactly what the gate has not established.
- **The launch activities are gated.** The plan's acquisition plan (Show HN, the
  subreddit threads, the X thread) must not run until the incident count reaches 2
  and at least 2 of 5 interviews report lost work. The repo can be published as a working prototype; it cannot be launched
  as a validated need.
- **The traction and absorption kill criteria still apply.** The 30-day traction
  gate (fewer than 50 stars and no organic issue or PR → kill) and the absorption
  gate (a mainstream harness ships per-resource leases plus deadlock detection →
  the window is closed) are unchanged by this build.

## Why a runnable prototype exists before the gate passed

The gate's own purpose is to make the interviews cheap and concrete: *"Weekend 2:
only if weekend 1 passes, build a thin prototype."* This build inverts that order
on purpose, and the inversion is a risk, not a justification:

- It makes the interview question answerable. "Have you lost work to a collision?"
  produces a shrug; `leasekeeper demo` in front of the developer produces a
  yes/no about their own setup in under a minute.
- It stays inside the plan's own build budget rather than adding new scope.

The risk is stated rather than hidden: a prototype invites building on an
unvalidated premise. If the interviews come back with fewer than 2 of 5 reporting
lost work, the correct outcome is to archive this repo and record that in this
file — not to look for a friendlier question.

## What would change this file to GO

1. Two or more citations of a real collision or circular wait, each with a URL and
   a quote from the person it happened to.
2. At least 2 of 5 interviewed developers reporting lost work to a resource
   collision, with the resource named.
3. No mainstream harness shipping per-resource leases plus deadlock detection at
   the time of the check.

When those are recorded here, replace this verdict, update the README's roadmap
section, and only then run the launch plan.
