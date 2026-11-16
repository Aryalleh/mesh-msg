// Mava in the browser (docs/web-access.md): the UI around window.mava, the
// Go client compiled to WebAssembly (web/cmd/webclient). Served by a Mava
// board at /app/; the board is the link to LoRa and the local mesh, relays
// are reached over HTTPS when the phone has internet.
"use strict";

const $ = (id) => document.getElementById(id);
const DEFAULT_RELAYS = "https://eu-1.mfadem.ir";
const state = { chat: null, statuses: {} };

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function setting(key, def) {
  try { return localStorage.getItem(key) ?? def; } catch { return def; }
}

function save(key, v) {
  try { localStorage.setItem(key, v); } catch {}
}

function time(ms) {
  return new Date(ms).toLocaleTimeString("fa-IR", { hour: "2-digit", minute: "2-digit" });
}

function header(title, back) {
  $("title").textContent = title;
  $("back").classList.toggle("hidden", !back);
}

function renderStatus() {
  const parts = Object.entries(state.statuses).map(([k, v]) => (v ? "●" : "○") + " " + k);
  $("status").textContent = parts.join("  ");
}

// ---- screens ----

function showWelcome() {
  header("Mava", false);
  $("composer").classList.add("hidden");
  $("main").innerHTML = `
    <div class="card">
      <p>به Mava خوش آمدید. این پیام‌رسان در خود مرورگر اجرا می‌شود؛ کلیدهای شما فقط روی همین دستگاه می‌مانند.</p>
      <p class="muted">Welcome. Your keys stay in this browser.</p>
      <label>نام شما<input id="name" dir="auto" autocomplete="nickname"></label>
      <p><button class="primary" id="go">شروع</button></p>
    </div>`;
  $("go").onclick = () => {
    save("mava.name", $("name").value.trim());
    boot();
  };
}

function showChats() {
  state.chat = null;
  header("گفتگوها", false);
  $("composer").classList.add("hidden");
  const contacts = JSON.parse(mava.contacts());
  let html = `<div class="card">`;
  if (!contacts.length) html += `<p class="muted">هنوز مخاطبی ندارید. لینک مخاطب را اضافه کنید یا لینک خودتان را بفرستید.</p>`;
  for (const c of contacts) {
    const last = JSON.parse(mava.messages(c.hash)).pop();
    html += `<div class="contact" data-h="${esc(c.hash)}"><div><b dir="auto">${esc(c.name)}</b>
      <div class="muted" dir="auto">${last ? esc(last.text.slice(0, 60)) : ""}</div></div>
      <span class="muted">${last ? time(last.time) : ""}</span></div>`;
  }
  html += `</div>
    <div class="card">
      <label>افزودن مخاطب (لینک یا meshmsg:)<input id="uri" class="ltr" placeholder="https://eu-1.mfadem.ir/add#…"></label>
      <label>نام (اختیاری)<input id="cname" dir="auto"></label>
      <p><button class="primary" id="add">افزودن</button></p>
    </div>
    <div class="card">
      <b>لینک شما</b> <span class="muted">برای اینکه دیگران شما را اضافه کنند</span>
      <p class="ltr" id="mylink">${esc(mava.link())}</p>
      <button class="link" id="share">اشتراک / کپی</button>
    </div>`;
  $("main").innerHTML = html;
  document.querySelectorAll(".contact").forEach((el) => (el.onclick = () => showChat(el.dataset.h)));
  $("add").onclick = () => {
    try {
      const c = JSON.parse(mava.addContact($("uri").value, $("cname").value));
      showChat(c.hash);
    } catch (e) {
      alert("لینک نامعتبر است\n" + e.message);
    }
  };
  $("share").onclick = async () => {
    const link = mava.link();
    if (navigator.share) {
      try { await navigator.share({ title: "Mava", text: link }); return; } catch {}
    }
    try { await navigator.clipboard.writeText(link); alert("کپی شد"); } catch { prompt("لینک شما:", link); }
  };
}

function showChat(hash) {
  state.chat = hash;
  const c = JSON.parse(mava.contacts()).find((x) => x.hash === hash);
  header(c ? c.name : hash.slice(0, 8), true);
  $("composer").classList.remove("hidden");
  renderMessages();
  $("text").focus();
}

function renderMessages() {
  if (!state.chat) return;
  const msgs = JSON.parse(mava.messages(state.chat));
  let html = `<div class="msgs">`;
  for (const m of msgs) {
    const tick = m.mine ? (m.status === "delivered" ? " ✓✓" : " ✓") : "";
    html += `<div class="msg ${m.mine ? "mine" : "theirs"}">${esc(m.text)}<small>${time(m.time)}${tick}</small></div>`;
  }
  html += `</div>`;
  if (!msgs.length) html = `<p class="muted">اولین پیام را بفرستید.</p>`;
  $("main").innerHTML = html;
  $("main").scrollTop = $("main").scrollHeight;
}

function showSettings() {
  state.chat = null;
  header("تنظیمات", true);
  $("composer").classList.add("hidden");
  $("main").innerHTML = `
    <div class="card">
      <label>نام شما<input id="sname" dir="auto" value="${esc(setting("mava.name", ""))}"></label>
      <label>رله‌ها (با ویرگول جدا)<input id="relays" class="ltr" value="${esc(setting("mava.relays", DEFAULT_RELAYS))}"></label>
      <p class="muted">رله‌ها وقتی گوشی اینترنت دارد استفاده می‌شوند؛ بدون اینترنت، پیام‌ها از راه برد Mava (LoRa) می‌روند.</p>
      <p><button class="primary" id="ssave">ذخیره</button></p>
    </div>
    <div class="card"><b>شناسه</b><p class="ltr">${esc(mava.hash())}</p>
      <p class="muted">کلیدها در حافظهٔ این مرورگر ذخیره شده‌اند. پاک کردن داده‌های سایت، هویت شما را پاک می‌کند.</p></div>`;
  $("ssave").onclick = () => {
    const name = $("sname").value.trim();
    save("mava.name", name);
    mava.setName(name);
    const r = $("relays").value.trim();
    if (r !== setting("mava.relays", DEFAULT_RELAYS)) {
      save("mava.relays", r);
      location.reload();
      return;
    }
    showChats();
  };
}

function send() {
  const t = $("text").value;
  if (!t.trim() || !state.chat) return;
  try {
    mava.send(state.chat, t);
    $("text").value = "";
    renderMessages();
  } catch (e) {
    alert(e.message);
  }
}

// ---- start ----

function onEvent(json) {
  const e = JSON.parse(json);
  if (e.kind === "link") {
    const [name, st] = e.detail.split(": ");
    const key = name === "board" ? "برد" : "رله";
    state.statuses[key] = st === "online";
    renderStatus();
    return;
  }
  if (state.chat && e.contact === state.chat) renderMessages();
  else if (!state.chat && $("title").textContent === "گفتگوها") showChats();
}

function boot() {
  if (!setting("mava.name", null) && !localStorage.getItem("mava.state")) return showWelcome();
  mava.onEvent(onEvent);
  // On a Mava board the page and /api/link share the origin; elsewhere "-" skips it.
  const onBoard = location.pathname.startsWith("/app/");
  mava.start(setting("mava.name", ""), setting("mava.relays", DEFAULT_RELAYS), setting("mava.board", onBoard ? "" : "-"));
  showChats();
}

async function load() {
  const go = new Go();
  const resp = await fetch("main.wasm");
  const bytes = await resp.arrayBuffer();  // works whatever MIME type the board sends
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  window.addEventListener("mava-ready", boot, { once: true });
  go.run(instance);
}

$("back").onclick = () => showChats();
$("menu").onclick = () => showSettings();
$("send").onclick = send;
$("text").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    send();
  }
});
load().catch((e) => {
  $("main").innerHTML = `<div class="card">بارگذاری نشد: ${esc(e.message)}</div>`;
});
