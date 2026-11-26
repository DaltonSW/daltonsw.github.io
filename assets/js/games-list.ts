// Game list tab: sortable column headers, title search, and the sidebar's
// Status/Tags/Platforms rows as filter toggles (one active per card; clicking the
// active one clears it). Rows carry their sort/filter keys as data-* attributes
// (games-list.html); without JS the table renders as-is, in its server-side order.

type Dir = "asc" | "desc";
const NUMERIC = new Set(["playtime", "ach", "rating"]);

function initGamesList(root: HTMLElement): void {
  const body = root.querySelector<HTMLElement>("[data-games-list-rows]");
  const controls = root.querySelector<HTMLElement>("[data-games-list-controls]");
  if (!body || !controls) return;

  const search = root.querySelector<HTMLInputElement>("[data-games-list-search]");
  const toggles = Array.from(root.querySelectorAll<HTMLButtonElement>("button[data-filter]"));
  const count = root.querySelector<HTMLElement>("[data-games-list-count]");
  const empty = root.querySelector<HTMLElement>("[data-games-list-empty]");
  const buttons = Array.from(root.querySelectorAll<HTMLButtonElement>("[data-sort]"));
  const rows = Array.from(body.querySelectorAll<HTMLElement>(".ps-row"));

  const active: Record<string, string> = { status: "", tag: "", platform: "" };

  let sortKey = "last";
  let sortDir: Dir = "desc";

  function value(row: HTMLElement, key: string): string {
    return row.dataset[key] ?? "";
  }

  function compare(a: HTMLElement, b: HTMLElement): number {
    const va = value(a, sortKey);
    const vb = value(b, sortKey);
    // empty values always sink, whichever way the column is sorted
    if (va === "" && vb !== "") return 1;
    if (vb === "" && va !== "") return -1;
    let cmp = 0;
    if (va !== "" && vb !== "") {
      cmp = NUMERIC.has(sortKey) ? Number(va) - Number(vb) : va.localeCompare(vb);
      if (sortDir === "desc") cmp = -cmp;
    }
    return cmp || value(a, "title").localeCompare(value(b, "title"));
  }

  function sort(): void {
    rows.sort(compare);
    body!.append(...rows);
    for (const btn of buttons) {
      const header = btn.parentElement!;
      if (btn.dataset.sort === sortKey) {
        header.setAttribute("aria-sort", sortDir === "asc" ? "ascending" : "descending");
      } else {
        header.removeAttribute("aria-sort");
      }
    }
  }

  function filter(): void {
    const query = (search?.value ?? "").trim().toLowerCase();
    let shown = 0;
    for (const row of rows) {
      const matchesQuery = query === "" || value(row, "title").includes(query);
      row.hidden = !(matchesQuery && matchesStatus(row) && matchesTag(row) && matchesPlatform(row));
      if (!row.hidden) shown++;
    }
    if (count) count.textContent = `${shown} of ${rows.length} games`;
    if (empty) empty.hidden = shown > 0;
  }

  function matchesStatus(row: HTMLElement): boolean {
    const wanted = active.status;
    if (wanted === "") return true;
    // mastered/unplayed are derived flags, not statuses
    if (wanted === "mastered" || wanted === "unplayed") return row.hasAttribute(`data-${wanted}`);
    // backlog also covers planned replays (the old Backlog tab's two sections)
    return value(row, "status") === wanted || (wanted === "backlog" && row.hasAttribute("data-planned"));
  }

  function matchesTag(row: HTMLElement): boolean {
    // "|"-joined: the game's tags plus those only some of its runs carry
    return active.tag === "" || value(row, "tags").split("|").includes(active.tag);
  }

  function matchesPlatform(row: HTMLElement): boolean {
    // a row's platform can be a comma-joined list, e.g. "pc, xbox 360"
    return active.platform === "" || value(row, "platform").split(", ").includes(active.platform);
  }

  for (const toggle of toggles) {
    toggle.addEventListener("click", () => {
      const kind = toggle.dataset.filter!;
      const val = toggle.dataset.value ?? "";
      active[kind] = active[kind] === val ? "" : val;
      for (const t of toggles) {
        if (t.dataset.filter === kind) t.setAttribute("aria-pressed", String(active[kind] === t.dataset.value));
      }
      filter();
    });
  }

  for (const btn of buttons) {
    btn.addEventListener("click", () => {
      const key = btn.dataset.sort!;
      if (key === sortKey) {
        sortDir = sortDir === "asc" ? "desc" : "asc";
      } else {
        sortKey = key;
        sortDir = (btn.dataset.sortDir as Dir) ?? "asc";
      }
      sort();
    });
  }
  search?.addEventListener("input", filter);

  controls.hidden = false;
  root.classList.add("games-list--js");
  sort();
  filter();
}

document.querySelectorAll<HTMLElement>("[data-games-list]").forEach(initGamesList);
