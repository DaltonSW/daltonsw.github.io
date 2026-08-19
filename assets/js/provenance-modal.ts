// Wires the "Where does the info come from?" trigger to a native <dialog>.

function initProvenanceModal(trigger: HTMLButtonElement): void {
  const dialogId = trigger.getAttribute("aria-controls");
  const dialog = dialogId ? document.getElementById(dialogId) : null;
  if (!(dialog instanceof HTMLDialogElement)) return;

  trigger.hidden = false;
  trigger.addEventListener("click", () => dialog.showModal());

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
