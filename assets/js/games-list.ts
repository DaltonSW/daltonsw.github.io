// Game list tab: sortable column headers plus title search and status filter.
// Rows carry their sort/filter keys as data-* attributes (games-list.html);
// without JS the table renders as-is, in its server-side order.

type Dir = "asc" | "desc";
const NUMERIC = new Set(["playtime", "ach", "rating"]);

function initGamesList(root: HTMLElement): void {
  const body = root.querySelector<HTMLElement>("[data-games-list-rows]");
  const controls = root.querySelector<HTMLElement>("[data-games-list-controls]");
  if (!body || !controls) return;

  const search = root.querySelector<HTMLInputElement>("[data-games-list-search]");
  const status = root.querySelector<HTMLSelectElement>("[data-games-list-status]");
  const count = root.querySelector<HTMLElement>("[data-games-list-count]");
  const empty = root.querySelector<HTMLElement>("[data-games-list-empty]");
  const buttons = Array.from(root.querySelectorAll<HTMLButtonElement>("[data-sort]"));
  const rows = Array.from(body.querySelectorAll<HTMLElement>(".ps-row"));

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
    const wanted = status?.value ?? "";
    let shown = 0;
    for (const row of rows) {
      const matchesQuery = query === "" || value(row, "title").includes(query);
      // the backlog filter also covers planned replays (the old Backlog tab's two sections)
      const matchesStatus =
        wanted === "" ||
        value(row, "status") === wanted ||
        (wanted === "backlog" && row.hasAttribute("data-planned"));
      row.hidden = !(matchesQuery && matchesStatus);
      if (!row.hidden) shown++;
    }
    if (count) count.textContent = `${shown} of ${rows.length} games`;
    if (empty) empty.hidden = shown > 0;
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
  status?.addEventListener("change", filter);

  controls.hidden = false;
  root.classList.add("games-list--js");
  sort();
  filter();
}

document.querySelectorAll<HTMLElement>("[data-games-list]").forEach(initGamesList);
