// Mava in the browser (docs/web-access.md): the UI around window.mava, the
// Go client compiled to WebAssembly (web/cmd/webclient). Served by a Mava
// board at /app/; the board is the link to LoRa and the local mesh, relays
// are reached over HTTPS when the phone has internet. Design: docs/design.md.
"use strict";

const $ = (id) => document.getElementById(id);
const DEFAULT_RELAYS = "https://eu-1.mfadem.ir";
const state = { chat: null, links: {}, screen: "", shown: 0 };

const icon = (name, cls = "") => `<svg class="i ${cls}" aria-hidden="true"><use href="#i-${name}"/></svg>`;

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

function initial(name) {
  const c = [...String(name).trim()][0] || "?";
  return esc(c.toUpperCase());
}

let toastTimer = 0;
function toast(text) {
  const t = $("toast");
  t.textContent = text;
  t.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove("show"), 3500);
}

function header(title, back, wordmark = false, menu = true) {
  $("menu").classList.toggle("hidden", !menu);
  $("title").innerHTML = wordmark ? `<span class="wordmark">MAVA</span>` : esc(title);
  $("back").classList.toggle("hidden", !back);
}

// Link status, not a choice: every message leaves on every link that is up,
// as this browser's identity. Icon + word, never colour alone.
function renderStatus() {
  const names = { board: ["radio", "برد"], relay: ["globe", "رله"] };
  $("status").innerHTML = Object.entries(state.links)
    .map(([k, on]) => `<span class="chip ${on ? "on" : ""}" title="${on ? "وصل" : "قطع"}">${icon(names[k][0])}${names[k][1]} ${on ? "وصل" : "قطع"}</span>`)
    .join("");
}

// ---- screens ----

function showWelcome() {
  state.screen = "welcome";
  header("", false, true, false);
  $("composer").classList.add("hidden");
  $("main").innerHTML = `
    <div class="card" style="text-align:center">
      <div class="wordmark" style="font-size:34px;margin:8px 0">MAVA</div>
      <p>پیام‌رسانی که بدون اینترنت هم کار می‌کند: از راه LoRa، بلوتوث و رله‌ها.</p>
      <p class="muted">${icon("lock")} کلیدهای شما فقط در همین مرورگر می‌مانند.</p>
    </div>
    <div class="card">
      <label for="name">نام شما (برای مخاطبان)</label>
      <input id="name" dir="auto" autocomplete="nickname" maxlength="40">
      <div class="row"><button class="btn primary" id="go">شروع</button></div>
    </div>`;
  $("go").onclick = () => {
    save("mava.name", $("name").value.trim());
    boot();
  };
  $("name").focus();
}

function showChats() {
  state.chat = null;
  state.screen = "chats";
  header("", false, true);
  $("composer").classList.add("hidden");
  const contacts = JSON.parse(mava.contacts());
  let list = "";
  for (const c of contacts) {
    const last = JSON.parse(mava.messages(c.hash)).pop();
    list += `<button class="contact" data-h="${esc(c.hash)}">
      <span class="avatar" aria-hidden="true">${initial(c.name)}</span>
      <span class="c-main"><span class="c-name" dir="auto">${esc(c.name)}</span>
      <span class="c-last" dir="auto">${last ? esc(last.text.slice(0, 80)) : "—"}</span></span>
      <span class="c-time">${last ? time(last.time) : ""}</span></button>`;
  }
  $("main").innerHTML = `
    <div class="card">
      <h2>${icon("chat")} گفتگوها</h2>
      ${list ? `<div class="list">${list}</div>` : `<div class="empty">هنوز گفتگویی ندارید.<br>لینک خودتان را بفرستید یا لینک یک مخاطب را اضافه کنید.</div>`}
    </div>
    <div class="card">
      <details ${contacts.length ? "" : "open"}>
        <summary>${icon("user-plus")} افزودن مخاطب</summary>
        <label for="uri">لینک مخاطب (https://…/add#… یا meshmsg:)</label>
        <input id="uri" class="ltr" dir="ltr" inputmode="url" autocapitalize="off" autocorrect="off" spellcheck="false">
        <label for="cname">نام (اختیاری)</label>
        <input id="cname" dir="auto">
        <div class="row"><button class="btn primary" id="add">${icon("user-plus")} افزودن</button></div>
      </details>
    </div>
    <div class="card">
      <h2>${icon("share")} لینک شما</h2>
      <p class="muted">این لینک را برای دیگران بفرستید تا شما را اضافه کنند.</p>
      <p class="ltr" id="mylink">${esc(mava.link())}</p>
      <div class="row"><button class="btn ghost" id="share">${icon("share")} اشتراک / کپی</button></div>
    </div>`;
  document.querySelectorAll(".contact").forEach((el) => (el.onclick = () => showChat(el.dataset.h)));
  $("add").onclick = () => {
    try {
      const c = JSON.parse(mava.addContact($("uri").value, $("cname").value));
      showChat(c.hash);
    } catch (e) {
      toast("لینک نامعتبر است — آن را کامل کپی کنید");
      $("uri").focus();
    }
  };
  $("share").onclick = async () => {
    const link = mava.link();
    if (navigator.share) {
      try { await navigator.share({ title: "Mava", text: link }); return; } catch {}
    }
    try { await navigator.clipboard.writeText(link); toast("لینک کپی شد"); } catch { prompt("لینک شما:", link); }
  };
}

function showChat(hash) {
  state.chat = hash;
  state.screen = "chat";
  const c = JSON.parse(mava.contacts()).find((x) => x.hash === hash);
  header(c ? c.name : hash.slice(0, 8), true);
  $("composer").classList.remove("hidden");
  state.shown = Infinity; // no entrance animation for the history
  renderMessages();
  state.shown = JSON.parse(mava.messages(hash)).length;
  $("text").focus();
}

function renderMessages() {
  if (!state.chat) return;
  const msgs = JSON.parse(mava.messages(state.chat));
  if (!msgs.length) {
    $("main").innerHTML = `<div class="empty">${icon("lock")}<br>پیام‌ها سرتاسر رمز می‌شوند.<br>اولین پیام را بفرستید.</div>`;
    return;
  }
  let html = `<div class="msgs" role="log" aria-live="polite">`;
  msgs.forEach((m, i) => {
    const tick = m.mine
      ? m.status === "delivered"
        ? `<span class="delivered">✓✓<span class="sr">رسید</span></span>`
        : `<span>✓<span class="sr">فرستاده شد</span></span>`
      : "";
    const fresh = i >= state.shown ? " fresh" : "";
    html += `<div class="msg ${m.mine ? "mine" : "theirs"}${fresh}">${esc(m.text)}<small>${time(m.time)} ${tick}</small></div>`;
  });
  state.shown = msgs.length;
  $("main").innerHTML = html + `</div>`;
  $("main").scrollTop = $("main").scrollHeight;
}

function showSettings() {
  state.chat = null;
  state.screen = "settings";
  header("تنظیمات", true);
  $("composer").classList.add("hidden");
  $("main").innerHTML = `
    <div class="card">
      <label for="sname">نام شما</label>
      <input id="sname" dir="auto" value="${esc(setting("mava.name", ""))}">
      <label for="relays">رله‌ها (با ویرگول جدا)</label>
      <input id="relays" class="ltr" dir="ltr" value="${esc(setting("mava.relays", DEFAULT_RELAYS))}" inputmode="url">
      <div class="row"><button class="btn primary" id="ssave">ذخیره</button></div>
    </div>
    <div class="card">
      <h2>${icon("radio")} برد یا رله؟ هر دو.</h2>
      <p class="muted">لازم نیست انتخاب کنید: هر پیام با همین هویتِ این مرورگر هم‌زمان از همهٔ مسیرهای وصل فرستاده می‌شود — از راه برد Mava (LoRa، بلوتوث و اینترنتِ خود برد) و، اگر گوشی اینترنت دارد، مستقیم به رله‌ها. گیرنده هر پیام را فقط یک بار می‌بیند. نشانه‌های بالای صفحه فقط نشان می‌دهند کدام مسیر الان وصل است.</p>
    </div>
    <div class="card">
      <h2>${icon("lock")} شناسه</h2>
      <p class="ltr">${esc(mava.hash())}</p>
      <p class="muted">کلیدها در حافظهٔ این مرورگر ذخیره شده‌اند. پاک کردن داده‌های سایت، هویت شما را پاک می‌کند.</p>
    </div>`;
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
    toast("ذخیره شد");
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
    toast("ارسال نشد: " + e.message);
  }
}

// ---- start ----

function onEvent(json) {
  const e = JSON.parse(json);
  if (e.kind === "link") {
    const [name, st] = e.detail.split(": ");
    state.links[name === "board" ? "board" : "relay"] = st === "online";
    renderStatus();
    return;
  }
  if (state.chat && e.contact === state.chat) renderMessages();
  else if (state.screen === "chats") showChats();
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
  const bytes = await resp.arrayBuffer(); // works whatever MIME type the board sends
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
  $("main").innerHTML = `<div class="card">بارگذاری نشد: ${esc(e.message)}<div class="row"><button class="btn ghost" onclick="location.reload()">دوباره</button></div></div>`;
});
