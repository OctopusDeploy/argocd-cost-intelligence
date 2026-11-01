# Contributors

Who works on Argo CD Cost Intelligence and how its pull requests get reviewed and merged. The page is generated each week from the repository's pull request activity by [oss-chronicle](https://github.com/ppapapetrou76/oss-chronicle). You can also [open it on its own page](assets/contributors/index.html).

<iframe id="contributors-page" src="../assets/contributors/" title="Contributors" style="width: 100%; min-height: 600px; border: 0;"></iframe>

<script>
(() => {
  const frame = document.getElementById("contributors-page");
  const theme = () => {
    const doc = frame.contentDocument;
    if (!doc) return;
    doc.documentElement.setAttribute("data-theme", document.body.getAttribute("data-md-color-scheme") === "slate" ? "dark" : "light");
  };
  new MutationObserver(theme).observe(document.body, {attributes: true, attributeFilter: ["data-md-color-scheme"]});
  frame.addEventListener("load", () => {
    const doc = frame.contentDocument;
    if (!doc) return;
    const fit = () => { frame.style.height = Math.ceil(doc.documentElement.getBoundingClientRect().height) + "px"; };
    new frame.contentWindow.ResizeObserver(fit).observe(doc.documentElement);
    fit();
    theme();
    history.replaceState(null, "", location.pathname + frame.contentWindow.location.search + location.hash);
    doc.addEventListener("click", (e) => {
      const a = e.target.closest("a[href]");
      if (a && a.origin !== location.origin) a.target = "_top";
    });
  });
  if (location.search) frame.src = "../assets/contributors/" + location.search;
})();
</script>
