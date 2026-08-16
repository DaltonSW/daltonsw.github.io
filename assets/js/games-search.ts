// Filters the "Game list" tab's rows by title as the user types into
// [data-games-search]. Rows live inside `.year-group` <details> elements;
// a group collapses entirely when none of its rows match, and forces itself
// open while a query is active so matches in collapsed years stay visible —
// each group's original open/closed state is restored once the query clears.

function initGamesSearch(input: HTMLInputElement): void {
  const panel = input.closest<HTMLElement>("#games-panel-list");
  if (!panel) return;

  const empty = panel.querySelector<HTMLElement>("[data-games-search-empty]");
  const emptyQuery = panel.querySelector<HTMLElement>("[data-games-search-query]");
  const groups = Array.from(panel.querySelectorAll<HTMLDetailsElement>(".year-group"));

  // Captured once, before any filtering, so clearing the query can restore it.
  const originalOpen = new Map<HTMLDetailsElement, boolean>();
  for (const group of groups) originalOpen.set(group, group.open);

  function applyFilter(): void {
    const query = input.value.trim().toLowerCase();
    let anyMatch = false;

    for (const group of groups) {
      const rows = Array.from(
        group.querySelectorAll<HTMLElement>(".ps-row:not(.ps-row--head)"),
      );

      let groupHasMatch = false;
      for (const row of rows) {
        const title = row.querySelector(".ps-row__title")?.textContent ?? "";
        const matches = query === "" || title.toLowerCase().includes(query);
        row.hidden = !matches;
        if (matches) groupHasMatch = true;
      }

      group.hidden = query !== "" && !groupHasMatch;
      if (query === "") {
        group.open = originalOpen.get(group) ?? group.open;
      } else if (groupHasMatch) {
        group.open = true;
        anyMatch = true;
      }
    }

    if (empty) empty.hidden = query === "" || anyMatch;
    if (emptyQuery) emptyQuery.textContent = input.value.trim();
  }

  input.addEventListener("input", applyFilter);
}

document
  .querySelectorAll<HTMLInputElement>("[data-games-search]")
  .forEach(initGamesSearch);
