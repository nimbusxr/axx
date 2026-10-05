// The depot desk's page. What it keeps, and its menu, come from the app around
// it: Electron's preload gives window.depot; under Tauri, its commands do.
"use strict";

const host = window.depot || tauriDepot();

function tauriDepot() {
  const { invoke } = window.__TAURI__.core;
  const { listen } = window.__TAURI__.event;
  return {
    load: () => invoke("load"),
    saveArrivals: (arrivals) => invoke("save_arrivals", { arrivals }),
    saveLevel: (level) => invoke("save_level", { level }),
    setCloseDayEnabled: (enabled) => invoke("set_close_day_enabled", { enabled }),
    onCloseDay: (fn) => listen("close-day", fn),
  };
}

const towns = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna",
  "Grimma", "Wurzen", "Eilenburg"];
const expected = Array.from({ length: 40 }, (_, i) => [`PX-DSK-${4101 + i}`, towns[i % towns.length]]);
const parcels = (n) => (n === 1 ? "1 parcel" : `${n} parcels`);
const $ = (id) => document.getElementById(id);

let arrivals = [];

function refresh() {
  $("register").disabled = $("reference").value.trim() === "";
  host.setCloseDayEnabled(arrivals.length > 0);
  const list = $("arrivals-list");
  list.replaceChildren(...arrivals.map((a) => {
    const li = document.createElement("li");
    li.textContent = a.reference;
    li.addEventListener("click", () => {
      $("status").textContent = `${a.reference}: ${a.level}${a.fragile ? ", fragile" : ""}`;
    });
    return li;
  }));
}

function level() {
  return document.querySelector('input[name="level"]:checked').value;
}

async function register() {
  const ref = $("reference").value.trim();
  if (!ref) return;
  if (arrivals.some((a) => a.reference === ref)) {
    $("status").textContent = `${ref} is already registered`;
    return;
  }
  const fragile = $("fragile").checked;
  arrivals.push({ reference: ref, level: level(), fragile });
  await host.saveArrivals(arrivals);
  await host.saveLevel(level());
  const printed = $("print-label").getAttribute("aria-checked") === "true";
  $("status").textContent = `Registered ${ref}: ${level()}${fragile ? ", fragile" : ""}${printed ? ", label printed" : ""}`;
  $("reference").value = "";
  $("fragile").checked = false;
  refresh();
}

async function closeDay() {
  const n = arrivals.length;
  arrivals = [];
  await host.saveArrivals(arrivals);
  $("status").textContent = `Day closed: ${parcels(n)} handed over`;
  refresh();
}

function showTab(name) {
  for (const tab of ["arrivals", "handover"]) {
    const on = tab === name;
    $(`tab-${tab}`).setAttribute("aria-selected", String(on));
    $(`tab-${tab}`).tabIndex = on ? 0 : -1;
    $(tab).hidden = !on;
  }
}

// Where the courier signs: a click puts a dot, a drag draws a stroke.
function signaturePad() {
  const pad = $("pad");
  const ctx = pad.getContext("2d");
  ctx.lineWidth = 3;
  ctx.lineCap = "round";
  let drawing = false;
  const at = (e) => [e.offsetX, e.offsetY];
  pad.addEventListener("mousedown", (e) => {
    drawing = true;
    ctx.beginPath();
    ctx.moveTo(...at(e));
    ctx.lineTo(...at(e));
    ctx.stroke();
    $("signed").textContent = "Signed";
  });
  pad.addEventListener("mousemove", (e) => {
    if (!drawing) return;
    ctx.lineTo(...at(e));
    ctx.stroke();
  });
  window.addEventListener("mouseup", () => { drawing = false; });
  $("clear").addEventListener("click", () => {
    ctx.clearRect(0, 0, pad.width, pad.height);
    $("signed").textContent = "Not signed";
  });
}

async function start() {
  const kept = await host.load();
  arrivals = kept.arrivals || [];
  document.querySelector(`input[name="level"][value="${kept.level === "Express" ? "Express" : "Standard"}"]`).checked = true;
  $("status").textContent = arrivals.length ? `${parcels(arrivals.length)} registered today` : "No parcels registered yet";

  $("expected").replaceChildren(...expected.map(([ref, town]) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${ref}</td><td>${town}</td>`;
    tr.addEventListener("click", () => {
      $("reference").value = ref;
      refresh();
    });
    return tr;
  }));

  $("reference").addEventListener("input", refresh);
  $("reference").addEventListener("keydown", (e) => {
    if (e.key === "Enter") register();
    if (e.key === "Escape") {
      $("reference").value = "";
      refresh();
    }
  });
  $("register").addEventListener("click", register);
  $("print-label").addEventListener("click", (e) => {
    const s = e.currentTarget;
    s.setAttribute("aria-checked", String(s.getAttribute("aria-checked") !== "true"));
  });
  $("tab-arrivals").addEventListener("click", () => showTab("arrivals"));
  $("tab-handover").addEventListener("click", () => showTab("handover"));
  $("rules-link").addEventListener("click", (e) => {
    e.preventDefault();
    $("rules").hidden = false;
  });
  signaturePad();
  host.onCloseDay(closeDay);
  refresh();
}

start();
