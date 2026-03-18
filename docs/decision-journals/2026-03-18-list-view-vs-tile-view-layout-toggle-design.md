# List view vs tile view — layout toggle design

- **ID:** 019d00f2-e3f5-75a5-98ef-bae93c8966d6
- **Short ID:** 019d00f2
- **Date:** 2026-03-18

## Journey

Considered three approaches: (1) a single layout with denser tile cards, (2) a toggle between tile grid and full list, (3) a user-configurable column count. Option 1 loses the ability to scan many services quickly in a row. Option 3 adds complexity with little payoff at typical service counts. Went with option 2 — separate ServiceRow component for list view, localStorage persistence so the choice survives page reloads, two icon-only toggle buttons (LayoutGrid / List from lucide-react) placed in the section header next to the running count. Spacer span in ServiceRow keeps columns aligned when watch badge is absent.

## What It Revealed

The ServiceCard and ServiceRow components share identical business logic (confirm-to-stop, confirm-to-remove, restart, logs) but have completely different layout concerns. Extracting shared logic into a hook would have been premature — the components are small enough that duplication is fine. The real win was localStorage persistence: without it the toggle resets on every reload, making it feel broken.
