document.addEventListener("DOMContentLoaded", () => {
  document.body.addEventListener("htmx:afterRequest", (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (!form.hasAttribute("data-reset-on-success")) return;
    if (event.detail.successful) form.reset();
  });

  const flash = document.querySelector("[data-auto-hide]");
  if (flash) {
    setTimeout(() => flash.remove(), 4000);
  }
});
