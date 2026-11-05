// GhostKey dashboard frontend. Vanilla JS, no build step, no dependency.
// Every value coming from the API is rendered with textContent (never
// innerHTML), so a crafted Secret name or Event message cannot inject markup.
"use strict";

(function () {
  const REFRESH_MS = 15000;
  const KL = "kl::";

  const main = document.getElementById("main");
  const updated = document.getElementById("updated");
  const banner = document.getElementById("demo-banner");
  const refreshBtn = document.getElementById("refresh");

  let refreshTimer = null;
  let renderSeq = 0;

  // ---------- DOM helpers ----------

  function h(tag, attrs, ...children) {
    const node = document.createElement(tag);
    if (attrs) {
      for (const [k, v] of Object.entries(attrs)) {
        if (v === undefined || v === null || v === false) continue;
        if (k === "class") node.className = v;
        else if (k === "text") node.textContent = v;
        else if (k.startsWith("on")) node.addEventListener(k.slice(2), v);
        else node.setAttribute(k, v === true ? "" : String(v));
      }
    }
    append(node, children);
    return node;
  }

  // children: a node, a string, or (nested) arrays of them.
  function append(node, children) {
    for (const c of [children].flat(Infinity)) {
      if (c === undefined || c === null || c === false) continue;
      node.appendChild(c instanceof Node ? c : document.createTextNode(String(c)));
    }
    return node;
  }

  function clear(node) {
    while (node.firstChild) node.removeChild(node.firstChild);
  }

  function pill(kind, text, title) {
    return h("span", { class: "pill " + kind, title }, text);
  }

  function nsName(ns, name) {
    return h("span", { class: "mono" }, h("span", { class: "muted" }, ns + "/"), name);
  }

  // ---------- formatting ----------

  const rtf = new Intl.RelativeTimeFormat("fr", { numeric: "auto" });
  const dtf = new Intl.DateTimeFormat("fr", { dateStyle: "medium", timeStyle: "medium" });

  function ago(iso) {
    if (!iso) return "—";
    const d = new Date(iso);
    const s = Math.round((d.getTime() - Date.now()) / 1000);
    const abs = Math.abs(s);
    if (abs < 60) return rtf.format(s, "second");
    if (abs < 3600) return rtf.format(Math.round(s / 60), "minute");
    if (abs < 86400) return rtf.format(Math.round(s / 3600), "hour");
    return rtf.format(Math.round(s / 86400), "day");
  }

  function timeEl(iso) {
    if (!iso) return h("span", { class: "muted" }, "—");
    return h("time", { datetime: iso, title: dtf.format(new Date(iso)) }, ago(iso));
  }

  function modePill(value) {
    switch (value) {
      case "enforce": return pill("ok", value);
      case "audit": return pill("warn", value, "Les violations sont journalisées mais la réécriture est autorisée");
      case "virtual-ip": return pill("info", value + " (expérimental)");
      default: return pill("neutral", value);
    }
  }

  // A shadow key: name, then the kl:: prefix in orange and a mask whose
  // length hints at the placeholder length. Only the length comes from the API.
  function shadowKey(k) {
    const chip = h("span", { class: "chip", title: k.prefix ? "Placeholder de " + k.length + " caractères" : "Placeholder sans préfixe kl::" });
    append(chip, [
      h("span", null, k.key),
      h("span", { class: "sep" }, "·"),
      k.prefix
        ? h("span", { class: "placeholder" }, h("span", { class: "kl" }, KL), h("span", { class: "mask" }, "•••"))
        : pill("deny", "sans kl::"),
      h("span", { class: "muted" }, k.length + " car."),
    ]);
    return chip;
  }

  // ---------- API ----------

  async function api(path, init) {
    const res = await fetch(path, Object.assign({ headers: { Accept: "application/json" }, cache: "no-store" }, init || {}));
    let body = null;
    try { body = await res.json(); } catch (_) { /* non-JSON */ }
    if (!res.ok) {
      throw new Error((body && body.error) || ("HTTP " + res.status));
    }
    banner.hidden = !body.demo;
    updated.textContent = "Mis à jour " + new Date(body.generatedAt).toLocaleTimeString("fr");
    return body.data;
  }

  function errorBox(err) {
    return h("div", { class: "error-box", role: "alert" }, h("strong", null, "Erreur : "), err.message || String(err));
  }

  function pageHead(title, subtitle, ...extra) {
    return h("div", { class: "page-head" },
      h("div", null, h("h1", null, title), subtitle ? h("p", { class: "muted" }, subtitle) : null),
      extra);
  }

  function table(headers, rows, emptyText) {
    if (!rows.length) return h("div", { class: "empty" }, emptyText);
    // On narrow screens rows become cards, each cell labeled by its header.
    for (const row of rows) {
      Array.from(row.children).forEach((td, i) => td.setAttribute("data-label", headers[i] || ""));
    }
    return h("div", { class: "table-wrap" },
      h("table", null,
        h("thead", null, h("tr", null, headers.map((t) => h("th", { scope: "col" }, t)))),
        h("tbody", null, rows)));
  }

  function searchBox(placeholder, onInput) {
    return h("input", {
      class: "search", type: "search", placeholder, "aria-label": placeholder,
      oninput: (e) => onInput(e.target.value.trim().toLowerCase()),
    });
  }

  // ---------- pages ----------

  async function renderOverview(root) {
    const ov = await api("/api/overview");
    const c = ov.counts;
    const stat = (value, label, kind) =>
      h("div", { class: "stat" + (kind ? " is-" + kind : "") }, h("div", { class: "value" }, String(value)), h("div", { class: "label" }, label));

    const ctl = ov.controller;
    const ctlKind = !ctl.found ? "deny" : ctl.readyNodes === ctl.desiredNodes && ctl.desiredNodes > 0 ? "ok" : "warn";
    const ctlPanel = h("section", { class: "panel" },
      h("div", { class: "panel-head" },
        h("h2", null, "Controller (DaemonSet)"),
        ctl.found ? pill(ctlKind, ctl.readyNodes + "/" + ctl.desiredNodes + " nœuds prêts") : pill("deny", "introuvable")),
      ctl.error ? h("div", { class: "panel-body" }, h("div", { class: "error-box" }, ctl.error)) : null,
      ctl.image ? h("div", { class: "panel-body" }, h("dl", { class: "kv" }, h("dt", null, "Image"), h("dd", { class: "mono" }, ctl.image))) : null,
      h("div", { class: "panel-body flush" },
        table(["Nœud", "Pod", "État", "Redémarrages", "Démarré"], ctl.nodes.map((n) => h("tr", null,
          h("td", { class: "mono" }, n.node || "—"),
          h("td", { class: "mono" }, n.pod),
          h("td", null, n.ready ? pill("ok", "Prêt") : pill(n.phase === "Running" ? "warn" : "deny", n.phase || "Inconnu")),
          h("td", null, String(n.restarts)),
          h("td", null, timeEl(n.startedAt)))), "Aucun pod controller trouvé.")));

    const wh = ov.webhook;
    const whKind = !wh.found ? "deny" : wh.readyReplicas >= 1 ? (wh.readyReplicas === wh.replicas ? "ok" : "warn") : "deny";
    const cfg = (ok) => (ok ? pill("ok", "configuré") : pill("deny", "absent"));
    const whPanel = h("section", { class: "panel" },
      h("div", { class: "panel-head" },
        h("h2", null, "Webhook"),
        wh.found ? pill(whKind, wh.readyReplicas + "/" + wh.replicas + " réplicas prêts") : pill("deny", "introuvable")),
      h("div", { class: "panel-body" },
        wh.error ? h("div", { class: "error-box" }, wh.error) : null,
        h("dl", { class: "kv" },
          h("dt", null, "Mutation des pods"), h("dd", null, cfg(wh.mutatingConfigured), " ", h("code", { class: "muted" }, "/mutate-pods")),
          h("dt", null, "Validation des Secrets"), h("dd", null, cfg(wh.validatingConfigured), " ", h("code", { class: "muted" }, "/validate-secrets")))));

    const m = ov.modes;
    const mode = (name, val) => h("div", { class: "mode" }, h("div", { class: "name" }, name), h("div", { class: "val" }, val));
    const modesPanel = h("section", { class: "panel" },
      h("div", { class: "panel-head" },
        h("h2", null, "Modes du data plane"),
        m.fromDaemonSet ? h("span", { class: "muted small" }, "lus dans les args du DaemonSet") : pill("warn", "valeurs par défaut (DaemonSet illisible)")),
      h("div", { class: "panel-body" },
        h("div", { class: "modes" },
          mode("eBPF", m.ebpfEnabled ? pill("ok", "activé") : pill("deny", "désactivé")),
          mode("secretBinding", modePill(m.secretBinding)),
          mode("sniCheck", modePill(m.sniCheck)),
          mode("hostResolution", modePill(m.hostResolution)),
          mode("tcAttachMode", modePill(m.tcAttachMode)))));

    append(root, [
      pageHead("Vue d'ensemble", "État de GhostKey dans le namespace " + ov.namespace + "."),
      h("div", { class: "stats" },
        stat(c.protectedSecrets, "Secrets protégés"),
        stat(c.trackedPods, "Pods suivis"),
        stat(c.missingShadows, "Shadows manquantes", c.missingShadows ? "deny" : "ok"),
        stat(c.violations, "Violations récentes", c.violations ? "warn" : "ok")),
      h("div", { class: "grid-2" }, ctlPanel, h("div", { class: "stack" }, whPanel, modesPanel)),
    ]);
  }

  async function renderSecrets(root) {
    const list = await api("/api/secrets");
    const body = h("div", { class: "panel-body flush" });
    const draw = (q) => {
      clear(body);
      const rows = list.filter((s) => !q || (s.namespace + "/" + s.name + " " + s.hosts).toLowerCase().includes(q)).map((s) => h("tr", null,
        h("td", null,
          nsName(s.namespace, s.name),
          s.problems.length ? h("ul", { class: "problems" }, s.problems.map((p) => h("li", null, p))) : null),
        h("td", { class: "mono" }, s.hosts || h("span", { class: "muted", title: "Aucune restriction d'hôte" }, "tout hôte")),
        h("td", { class: "mono" }, s.port || h("span", { class: "muted" }, "tout port")),
        h("td", null,
          s.shadow.exists ? pill("ok", "présente") : pill("deny", "manquante"),
          h("div", { class: "sub mono" }, s.shadow.name)),
        h("td", null, s.shadow.keys.length ? h("div", { class: "chips" }, s.shadow.keys.map(shadowKey)) : h("span", { class: "muted" }, "—")),
        h("td", null, s.pods.length
          ? h("div", { class: "chips" }, s.pods.map((p) => h("span", { class: "chip" }, p.name)))
          : h("span", { class: "muted" }, "aucun"))));
      append(body, table(["Secret", "Hôte autorisé", "Port", "Shadow", "Clés", "Pods"], rows,
        list.length ? "Aucun secret ne correspond au filtre." : "Aucun Secret labellisé ghostkey.io/enabled=true."));
    };
    draw("");
    append(root, [
      pageHead("Secrets protégés", "Secrets labellisés ghostkey.io/enabled=true et leur shadow <nom>-ghostkey.",
        h("div", { class: "toolbar" }, searchBox("Filtrer par nom ou hôte", draw))),
      h("section", { class: "panel" }, body),
    ]);
  }

  async function renderPods(root) {
    const list = await api("/api/pods");
    const body = h("div", { class: "panel-body flush" });
    const draw = (q) => {
      clear(body);
      const rows = list.filter((p) => !q || (p.namespace + "/" + p.name + " " + p.node).toLowerCase().includes(q)).map((p) => h("tr", null,
        h("td", { class: "mono" }, p.name),
        h("td", { class: "mono" }, p.namespace),
        h("td", { class: "mono" }, p.node || h("span", { class: "muted" }, "non planifié")),
        h("td", null, p.ready ? pill("ok", "Prêt") : pill(p.phase === "Running" ? "warn" : "deny", p.phase === "Running" ? "Non prêt" : p.phase || "Inconnu")),
        h("td", null, p.secrets.length
          ? h("div", { class: "chips" }, p.secrets.map((s) => h("span", { class: "chip" },
            s.secret, h("span", { class: "sep" }, "·"), h("span", { class: "muted" }, s.keys.length ? s.keys.join(", ") : "toutes les clés"))))
          : h("span", { class: "muted" }, "aucun")),
        h("td", null, timeEl(p.createdAt))));
      append(body, table(["Pod", "Namespace", "Nœud", "État", "Secrets liés", "Créé"], rows,
        list.length ? "Aucun pod ne correspond au filtre." : "Aucun pod annoté ghostkey.io/enabled=true."));
    };
    draw("");
    append(root, [
      pageHead("Pods", "Pods annotés ghostkey.io/enabled=true par le webhook. Un pod ne peut utiliser que les secrets qu'il référence.",
        h("div", { class: "toolbar" }, searchBox("Filtrer par pod, namespace ou nœud", draw))),
      h("section", { class: "panel" }, body),
    ]);
  }

  async function renderViolations(root) {
    const list = await api("/api/violations");
    const items = list.map((v) => h("li", { class: "event" + (v.audit ? " audit" : "") },
      h("div", { class: "bar" }),
      h("div", { class: "content" },
        h("div", { class: "meta" },
          v.audit ? pill("warn", "Audit : réécriture autorisée") : pill("deny", "Réécriture refusée"),
          h("span", { class: "chip" }, v.reason),
          nsName(v.namespace, v.pod),
          v.count > 1 ? h("span", { class: "muted small" }, "×" + v.count) : null,
          h("span", { class: "muted small" }, timeEl(v.time)),
          v.reporter ? h("span", { class: "muted small mono" }, v.reporter) : null),
        h("p", { class: "msg" }, v.message))));
    append(root, [
      pageHead("Violations", "Events SecretBindingViolation et SNICheckViolation publiés par le controller (même limitation de débit que les logs). Les Events expirent après environ une heure."),
      h("section", { class: "panel" },
        items.length ? h("ul", { class: "events" }, items) : h("div", { class: "empty" }, "Aucune violation récente.")),
    ]);
  }

  const SAMPLE = [
    "apiVersion: v1",
    "kind: Secret",
    "metadata:",
    "  name: api-credentials",
    "  namespace: my-app",
    "  labels:",
    '    ghostkey.io/enabled: "true"',
    "  annotations:",
    '    ghostkey.io/hosts: "api.stripe.com"',
    '    ghostkey.io/port: "443"',
    "stringData:",
    "  api-key: sk-test-0123456789abcdef",
    "",
  ].join("\n");

  function renderValidate(root) {
    const ta = h("textarea", {
      id: "secret-yaml", spellcheck: "false", autocomplete: "off", "aria-label": "YAML du Secret",
      placeholder: SAMPLE,
    });
    const result = h("div", { "aria-live": "polite" },
      h("div", { class: "verdict" }, h("p", { class: "muted" }, "Collez un Secret puis cliquez sur « Valider ».")));
    const btn = h("button", { class: "btn btn-primary", type: "button" }, "Valider");

    async function run() {
      btn.disabled = true;
      clear(result);
      try {
        const r = await api("/api/validate", {
          method: "POST",
          headers: { "Content-Type": "application/yaml", Accept: "application/json" },
          body: ta.value,
        });
        const ok = r.allowed;
        append(result, h("div", { class: "verdict " + (ok ? "ok" : "deny") },
          h("div", { class: "title" }, ok ? (r.enabled ? "Accepté" : "Accepté (non géré par GhostKey)") : "Refusé"),
          h("p", { class: "mono" }, r.message),
          !r.enabled ? h("p", { class: "muted small" }, "Sans le label ghostkey.io/enabled=true, le webhook laisse passer le Secret sans le protéger.") : null));
      } catch (err) {
        append(result, h("div", { class: "verdict deny" }, h("div", { class: "title" }, "YAML invalide"), h("p", null, err.message)));
      } finally {
        btn.disabled = false;
      }
    }
    btn.addEventListener("click", run);
    ta.addEventListener("keydown", (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "Enter") run();
    });

    append(root, [
      pageHead("Valider un Secret", "Même verdict que le webhook /validate-secrets, sans rien créer dans le cluster."),
      h("div", { class: "validate-grid" },
        h("section", { class: "panel" },
          h("div", { class: "panel-head" }, h("h2", null, "YAML du Secret"), h("span", { class: "muted small" }, "Ctrl+Entrée pour valider")),
          h("div", { class: "panel-body" },
            ta,
            h("div", { class: "actions" },
              btn,
              h("button", { class: "btn", type: "button", onclick: () => { ta.value = SAMPLE; ta.focus(); } }, "Insérer un exemple"),
              h("button", { class: "btn btn-ghost", type: "button", onclick: () => { ta.value = ""; ta.focus(); } }, "Effacer")),
            h("p", { class: "muted small" }, "Le YAML est envoyé au dashboard uniquement pour ce calcul : il n'est ni journalisé, ni conservé, ni renvoyé."))),
        h("div", { class: "stack" },
          result,
          h("section", { class: "panel" },
            h("div", { class: "panel-head" }, h("h2", null, "Règles vérifiées")),
            h("div", { class: "panel-body" },
              h("ul", { class: "rules" },
                h("li", null, "ghostkey.io/hosts et ghostkey.io/port en annotations, pas en labels"),
                h("li", null, "Un seul hôte (nom DNS ≤ 63 octets, IP ou *)"),
                h("li", null, "Port PORT ou PORT/tcp|udp"),
                h("li", null, "Chaque valeur fait de 8 à 128 octets"),
                h("li", null, "Une shadow de même longueur (et même longueur Huffman HPACK) existe")))))),
    ]);
    if (!ta.value) ta.focus();
  }

  const PAGES = {
    overview: { render: renderOverview, auto: true },
    secrets: { render: renderSecrets, auto: false },
    pods: { render: renderPods, auto: false },
    violations: { render: renderViolations, auto: true },
    validate: { render: renderValidate, auto: false },
  };

  function currentPage() {
    const name = (location.hash.replace(/^#\/?/, "") || "overview").split("?")[0];
    return PAGES[name] ? name : "overview";
  }

  async function render(background) {
    const name = currentPage();
    const page = PAGES[name];
    const seq = ++renderSeq;
    for (const a of document.querySelectorAll(".tabs a")) {
      if (a.dataset.page === name) a.setAttribute("aria-current", "page");
      else a.removeAttribute("aria-current");
    }
    document.title = document.querySelector('.tabs a[data-page="' + name + '"]').textContent + " · GhostKey";
    refreshBtn.hidden = name === "validate";

    const next = h("div");
    if (!background) {
      clear(main);
      append(main, h("div", { class: "loading" }, "Chargement…"));
    }
    try {
      await page.render(next);
    } catch (err) {
      clear(next);
      append(next, errorBox(err));
    }
    if (seq !== renderSeq) return; // a newer navigation won
    clear(main);
    append(main, next);

    clearTimeout(refreshTimer);
    if (page.auto) refreshTimer = setTimeout(() => render(true), REFRESH_MS);
  }

  refreshBtn.addEventListener("click", () => render(false));
  window.addEventListener("hashchange", () => { render(false); main.focus({ preventScroll: true }); });
  render(false);
})();
