// Wires the games-page "Where does the info come from?" trigger to a native
// <dialog>. showModal()/close() aren't available declaratively yet, so this
// is the minimal glue; <dialog> itself provides focus-trap, Esc-to-close,
// and the backdrop for free.

function initProvenanceModal(trigger: HTMLButtonElement): void {
  const dialogId = trigger.getAttribute("aria-controls");
  const dialog = dialogId ? document.getElementById(dialogId) : null;
  if (!(dialog instanceof HTMLDialogElement)) return;

  trigger.hidden = false;
  trigger.addEventListener("click", () => dialog.showModal());

  // Click on the ::backdrop (not on dialog content) closes it.
  dialog.addEventListener("click", (e: MouseEvent) => {
    if (e.target === dialog) dialog.close();
  });

  dialog
    .querySelector<HTMLButtonElement>("[data-provenance-close]")
    ?.addEventListener("click", () => dialog.close());

  dialog.addEventListener("close", () => trigger.focus());
}

document
  .querySelectorAll<HTMLButtonElement>("[data-provenance-open]")
  .forEach(initProvenanceModal);
