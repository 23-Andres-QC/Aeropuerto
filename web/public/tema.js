// Aplica el tema antes del primer pintado para evitar parpadeo. Va en un archivo aparte (y no en línea en el HTML)
// para que la política de seguridad del servidor (CSP) pueda exigir script-src 'self'.
(function () {
  try {
    var stored = localStorage.getItem("lap-theme");
    var theme = stored || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    document.documentElement.setAttribute("data-theme", theme);
  } catch (e) {}
})();
