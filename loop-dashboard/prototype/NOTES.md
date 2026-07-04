# Loop Dashboard — UI Prototype

## Question
What should the loop-engineering dashboard look like?

A personal dashboard for maintaining loop-engineering automations. Each loop is
defined as a YAML file (like GitHub Actions), runs on a **cron** schedule, and fires
either by submitting a goal to pi-ramid or by invoking `pi` directly (pi as PoC).
The focused story is now **post-merge-cleanup + pr-maintainer**: after a PR
merges, the loop scans follow-up debt, enumerates PR review threads, and gates
when human judgment is required. The dashboard is the control surface for those
loops — and for the **patterns** they reference (the registry that lives in
`patterns/registry.yaml`).

## Plan
Three structurally different variants on a single throwaway route, switchable
via `?variant=` with a floating bottom bar. Mock data only, in-memory, no
persistence. Read-only re: pi-ramid (no real goal submission) but **editable**
in-session: add/edit loops and patterns against the real schema.

Run:
```
cd prototype && python3 -m http.server 8099
# open http://localhost:8099/dashboard.html
# floating bottom bar: ◀ prev · [A|B|C] · next ▶  ·  or ?variant=A|B|C
```

## Variants (the primary affordance)
- **A — Lifecycle board**: loops as cards moving across phase columns
  (Scheduled → Running → Verifying → Human Gate → Done). Emphasis: the loop
  lifecycle as a board you watch loop.
- **B — Cadence timeline**: horizontal time rail with cron-fire pulses per
  loop track, like a train schedule. Emphasis: when each loop fires next.
- **C — YAML authoring console**: file tree of loop defs **and** pattern defs,
  mock YAML editor in the center, live run inspector on the right. Emphasis:
  "define YAML like Actions and maintain."

## Authoring flows (both modes round-trip)
The `+ Loop` / `+ Pattern` header buttons open a modal with two **mode tabs** —
the same validation surface, two contracts:

### Loop contract (`loops/<id>.yaml`)
Validates against a pi-ramid-style task contract + loop-engineering fields:
`version`(=1), `pattern`(must resolve to a registry id), `cron` (5-field,
GitHub-Actions-style **UTC** — computed next fire shown inline), `autonomy`
(L1|L2|L3), `trigger` (piramid|pi), `goal` (non-empty block), `project`
(absolute path), `human_gates` (≥1), `token.daily_cap` (>0), `id` (auto if
absent, must be unique).

### Pattern contract (`patterns/<id>.yaml`)
Validates against the **real `patterns/registry.schema.json`** from the
loop-engineering repo: `id` (`^[a-z][a-z0-9-]*$`, unique), `name` (≥3), `file`
(`^[a-z0-9-]+\.md$`), `goal` (block, ≥10 chars), `cadence`
(`^[0-9]+[mhd](-[0-9]+[mhd])?$`), `risk` (low|medium|high), `tools` (from the
schema's enum), `skills` (≥1, each ≥2 chars), `state` (`^[A-Za-z0-9-]+\.md$`),
`phases` (≥2), `human_gates` (≥1), `week_one_mode` (L1|L2|L3), `token_cost`
(low|medium|high|very-high), optional `cost:` block (5 sub-keys with minima).

Both modes give live per-field ✓/✗ rows + a precise error list; Add/Save is
gated until clean. Each row shows a schema-derived reason on failure ("must
match…", "must be …", "≥N") so the validator doubles as the schema doc.

Editing an existing loop/pattern prerounds the YAML and mutates in-place
("Edit …" title, "Save" button). The default loop template is a
`post-merge-cleanup` instance using the `pr-maintainer` skill; the default
pattern template is a valid `post-merge-cleanup-pr-maintainer` variant.

## Human gate interaction
A gated loop card gets a yellow **needs decision** state and the header shows a
pending-gates badge. Clicking either opens the human-gate modal. For the focused
story, `LOOP-05` pauses on `unresolved-feedback` after `pr-maintainer` enumerates
PR #182 review threads.

The modal shows:
- trigger, stalled phase, and why the gate fired;
- a pr-maintainer-style thread ledger (`file:line`, author, reviewed commit,
  comment body, classification);
- four explicit decisions: **Approve & resolve**, **Route as task**, **Defer**,
  **Reject**;
- the exact downstream gh-style action preview for each decision before the
  human clicks;
- a note box whose text becomes the agent's in-thread reply for route/reject.

A decision appends to the loop's run trail and re-renders the board behind the
modal. `Route as task`, for example, appends `wait`, removes `LOOP-05` from the
Human Gate column, and leaves the anti-skip ledger visible in the modal.

## Cron (the schedule spine)
Loops carry a `cron` field (GitHub-Actions-style 5-field UTC). A small parser
computes the **next fire** from a fixed `NOW` mock; variants B/C surface it as a
`next fire: HH:MM · cron <expr>` tooltip/text. The descriptive `cadence` from
the pattern is kept only as a human label — the real schedule is cron.

## What I learned (throwaway notes)
- Two natural authoring surfaces — loops (the running instances) vs patterns
  (the reusable registry) — share one modal + validator chassis but two
  contracts. The mode tab is the only top-level switch; everything below
  (template, validator, commit) dispatches on it.
- Cron-as-schedule is the right spine: it lets variant B answer "when does each
  thing fire next" precisely, and lets the loop editor show a live next-fire in
  the validation row.
- The brittle per-hour position lookup the timeline used before was a smell —
  once loops carry cron, next-fire is a single computed value, not a hand-mapped
  hour offset.

## Verdict
TBD — pick a winner (or a mix) once reviewed.