// Mava in the browser (docs/web-access.md): the UI around window.mava, the
// Go client compiled to WebAssembly (web/cmd/webclient). Served by a Mava
// board at /app/; the board is the link to LoRa and the local mesh, relays
// are reached over HTTPS when the phone has internet. Design: docs/design.md.
"use strict";

const $ = (id) => document.getElementById(id);
// eu-1 by name, and by its IP address for networks where the name is blocked
// (the board serves this page over http, so an http relay is allowed).
const DEFAULT_RELAYS = "https://eu-1.mfadem.ir,http://87.106.5.192";
const state = { chat: null, links: {}, screen: "", shown: 0, board: false };

// ---- the board's account (one account on the device) ----
// On a Mava board with its own messenger, this page shows the board's chats
// and sends as the board (same identity as the handheld screen and its QR
// code), after the device password. Same interface as the in-browser client
// (window.mava from main.wasm), so the screens below serve both.
class BoardAccount {
  constructor() {
    this.me = {};
    this.list = [];
    this.msgs = {};
    this.rev = -1;
    this.cb = () => {};
  }
  async api(path, body) {
    // A board busy for a moment (radio, Wi-Fi scan) drops a request now and
    // then: try again before calling it unreachable.
    let r;
    for (let i = 0; ; i++) {
      const ctl = new AbortController();
      const t = setTimeout(() => ctl.abort(), 10000);
      try {
        r = await fetch("/api/acct/" + path, { ...(body ? { method: "POST", body: new URLSearchParams(body) } : {}), signal: ctl.signal });
        break;
      } catch (e) {
        if (i >= 2) throw new Error("برد جواب نمی‌دهد");
        await new Promise((ok) => setTimeout(ok, 700));
      } finally {
        clearTimeout(t);
      }
    }
    if (r.status === 401) throw new Error("login");
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error || "HTTP " + r.status);
    return j;
  }
  async refresh() {
    this.me = await this.api("me");
    this.list = await this.api("chats");
    if (state.chat) await this.load(state.chat);
  }
  async load(hash) {
    this.msgs[hash] = await this.api("messages?h=" + encodeURIComponent(hash));
  }
  contacts() {
    // People we talk to first, newest first; people heard nearby after.
    const l = [...this.list].sort((a, b) => (a.saved !== b.saved ? (a.saved ? -1 : 1) : b.last - a.last));
    return JSON.stringify(l.map((c) => ({ hash: c.hash, name: c.saved ? c.name : c.name + " · نزدیک" })));
  }
  messages(hash) {
    if (this.msgs[hash]) return JSON.stringify(this.msgs[hash]);
    const c = this.list.find((x) => x.hash === hash);
    return JSON.stringify(c && c.preview ? [c.preview] : []);
  }
  link() { return this.me.link || ""; }
  hash() { return this.me.hash || ""; }
  name() { return this.me.name || ""; }
  async addContact(uri, name) {
    const c = await this.api("add", { link: uri, name });
    await this.refresh();
    return JSON.stringify(c);
  }
  async send(hash, text) {
    await this.api("send", { h: hash, text });
    await this.load(hash);
  }
  async setName(name) { this.me = await this.api("name", { name }); }
  onEvent(cb) { this.cb = cb; }
  async status() { return this.api("status"); }
  async setWifi(ssid, pass) { return this.api("wifi", { ssid, pass }); }
  async start() {
    let fails = 0, tick = 0;
    for (;;) {
      try {
        const { rev } = await this.api("rev");
        fails = 0;
        hideHint();
        if (!state.links.board) {
          state.links.board = true;
          renderStatus();
        }
        // The board's own relay link, every ~10 s.
        if (tick++ % 5 === 0) {
          const st = await this.status().catch(() => null);
          const up = !!(st && String(st.relay).startsWith("online"));
          if (st && state.links.relay !== up) {
            state.links.relay = up;
            renderStatus();
          }
        }
        if (rev !== this.rev) {
          this.rev = rev;
          await this.refresh();
          this.cb(JSON.stringify({ kind: "update" }));
        }
      } catch (e) {
        if (e.message === "login") return showLogin();
        if (++fails >= 2) showHint();
        if (state.links.board) {
          state.links.board = false;
          renderStatus();
        }
      }
      await new Promise((r) => setTimeout(r, 2000));
    }
  }
}

// The page loaded but the board stopped answering. The usual reason on a
// phone: this Wi-Fi has no internet, so the phone sends traffic over mobile
// data instead, where the board is not.
function showHint() {
  if ($("hint")) return;
  const d = document.createElement("div");
  d.id = "hint";
  d.className = "card";
  d.setAttribute("role", "alert");
  d.innerHTML = `<b>برد جواب نمی‌دهد.</b>
    <p class="muted">اگر دادهٔ همراه گوشی روشن است، چون این Wi-Fi اینترنت ندارد گوشی ممکن است درخواست‌ها را از دادهٔ همراه بفرستد و به برد نرسند: دادهٔ همراه را خاموش کنید، یا وقتی گوشی پرسید «این Wi-Fi اینترنت ندارد»، «ماندن / Keep» را بزنید. اگر برد تازه راه‌اندازی شده، چند ثانیه صبر کنید.</p>
    <p class="muted ltr" dir="ltr">The board is not answering. With mobile data on, the phone may send this page's requests over mobile data because this Wi-Fi has no internet: turn mobile data off, or choose "Keep Wi-Fi / Stay connected".</p>`;
  $("main").prepend(d);
}

function hideHint() {
  const h = $("hint");
  if (h) h.remove();
}

function showLogin(wrong) {
  state.screen = "login";
  header("", false, true, false);
  $("composer").classList.add("hidden");
  $("main").innerHTML = `
    <div class="card">
      <h2>${icon("lock")} حساب این دستگاه</h2>
      <p class="muted">این برد پیام‌رسان خودش را دارد: همان حسابی که روی صفحهٔ دستگاه و کد QR «مخاطب من» است. با رمز دستگاه وارد شوید تا از همین گوشی با همان حساب پیام بدهید.</p>
      <label for="pw">رمز دستگاه</label>
      <input id="pw" type="password" class="ltr" dir="ltr" autocomplete="current-password">
      ${wrong ? `<p class="muted" role="alert">${esc(wrong)}</p>` : ""}
      <div class="row"><button class="btn primary" id="login">${icon("lock")} ورود</button></div>
    </div>`;
  const go = async () => {
    const r = await fetch("/api/acct/login", { method: "POST", body: new URLSearchParams({ password: $("pw").value }) });
    if (r.ok) return startBoard();
    showLogin(r.status === 429 ? "چند دقیقه بعد دوباره امتحان کنید." : "رمز اشتباه است.");
  };
  $("login").onclick = go;
  $("pw").addEventListener("keydown", (e) => e.key === "Enter" && go());
  $("pw").focus();
}

async function startBoard() {
  const acct = new BoardAccount();
  try {
    await acct.refresh();
  } catch (e) {
    return e.message === "login" ? showLogin() : Promise.reject(e);
  }
  state.board = true;
  window.mava = acct;
  acct.onEvent(onEvent);
  acct.start();
  showChats();
}

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
  $("add").onclick = async () => {
    try {
      const c = JSON.parse(await mava.addContact($("uri").value, $("cname").value));
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

async function showChat(hash) {
  state.chat = hash;
  if (mava.load) await mava.load(hash).catch(() => {});
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
      <input id="sname" dir="auto" value="${esc(state.board ? mava.name() : setting("mava.name", ""))}">
      <div class="${state.board ? "hidden" : ""}">
      <label for="relays">رله‌ها (با ویرگول جدا)</label>
      <input id="relays" class="ltr" dir="ltr" value="${esc(setting("mava.relays", DEFAULT_RELAYS))}" inputmode="url">
      </div>
      <div class="row"><button class="btn primary" id="ssave">ذخیره</button></div>
    </div>
    ${state.board ? `<div class="card">
      <h2>${icon("globe")} Wi-Fi و رلهٔ برد</h2>
      <p class="muted" id="bstat">…</p>
      <label for="wssid">نام شبکه (SSID)</label>
      <input id="wssid" class="ltr" dir="ltr" list="wnets" autocomplete="off"><datalist id="wnets"></datalist>
      <label for="wpass">رمز شبکه</label>
      <input id="wpass" type="password" class="ltr" dir="ltr" autocomplete="off">
      <p class="muted">برد به این شبکه وصل می‌شود تا از رله پیام بفرستد و بگیرد. هات‌اسپات خود برد کنارش روشن می‌ماند، پس این گوشی وصل می‌ماند (شاید چند ثانیه قطع و وصل شود).</p>
      <div class="row"><button class="btn primary" id="wsave">ذخیرهٔ Wi-Fi</button></div>
    </div>` : ""}
    <div class="card">
      <h2>${icon("radio")} برد یا رله؟ هر دو.</h2>
      <p class="muted">لازم نیست انتخاب کنید: هر پیام با همین هویتِ این مرورگر هم‌زمان از همهٔ مسیرهای وصل فرستاده می‌شود — از راه برد Mava (LoRa، بلوتوث و اینترنتِ خود برد) و، اگر گوشی اینترنت دارد، مستقیم به رله‌ها. گیرنده هر پیام را فقط یک بار می‌بیند. نشانه‌های بالای صفحه فقط نشان می‌دهند کدام مسیر الان وصل است.</p>
    </div>
    <div class="card">
      <h2>${icon("lock")} شناسه</h2>
      <p class="ltr">${esc(mava.hash())}</p>
      <p class="muted">${state.board
        ? "حساب روی خود برد است (همان حساب صفحهٔ دستگاه)؛ این مرورگر فقط آن را نشان می‌دهد. پیام‌ها از LoRa، بلوتوث و رلهٔ برد می‌روند."
        : "کلیدها در حافظهٔ این مرورگر ذخیره شده‌اند. پاک کردن داده‌های سایت، هویت شما را پاک می‌کند."}</p>
    </div>`;
  if (state.board) boardWifi();
  $("ssave").onclick = async () => {
    const name = $("sname").value.trim();
    if (state.board) {
      await mava.setName(name);
      toast("ذخیره شد");
      return showChats();
    }
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

async function boardWifi() {
  const wifiText = { hotspot: "فقط هات‌اسپات (شبکه‌ای ذخیره نشده یا کلید راه‌اندازی روشن است)", joined: "وصل به", joining: "در حال وصل شدن به", off: "خاموش" };
  const fill = async (first) => {
    const st = await mava.status().catch(() => null);
    if (!st || state.screen !== "settings") return;
    const where = st.ssid && st.wifi !== "hotspot" ? ` «${esc(st.ssid)}»` : "";
    $("bstat").innerHTML = `Wi-Fi: ${esc(wifiText[st.wifi] || st.wifi)}${where}${st.ip ? ` (${esc(st.ip)})` : ""}<br>رله: ${esc(st.relay)}<br>هات‌اسپات: ${esc(st.hotspot)}${st.switch ? "<br>کلید راه‌اندازی روشن است: تا روشن است برد به Wi-Fi وصل نمی‌شود." : ""}`;
    if (first && !$("wssid").value) $("wssid").value = st.ssid || "";
    $("wnets").innerHTML = (st.nets || []).map((n) => `<option value="${esc(n)}">`).join("");
    if (first && !(st.nets || []).length) setTimeout(() => fill(false), 4000); // the scan takes a few seconds
  };
  fill(true);
  $("wsave").onclick = async () => {
    const ssid = $("wssid").value.trim(), pass = $("wpass").value;
    if (!ssid) return toast("نام شبکه را بنویسید");
    if (pass && pass.length < 8) return toast("رمز Wi-Fi دست‌کم ۸ نویسه است");
    try {
      await mava.setWifi(ssid, pass);
      toast("ذخیره شد؛ برد به " + ssid + " وصل می‌شود");
      setTimeout(() => fill(false), 8000);
    } catch (e) {
      toast("ذخیره نشد: " + e.message);
    }
  };
}

async function send() {
  const t = $("text").value;
  if (!t.trim() || !state.chat) return;
  try {
    await mava.send(state.chat, t);
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
  if (e.kind === "update") {  // the board's account changed
    if (state.chat) renderMessages();
    else if (state.screen === "chats") showChats();
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
  if (!resp.ok) throw new Error("main.wasm: HTTP " + resp.status);
  let bytes = await resp.arrayBuffer(); // works whatever MIME type the board sends
  // Still gzip (a board firmware before 0.1.1-beta11 forgot Content-Encoding): unpack here.
  const head = new Uint8Array(bytes, 0, 2);
  if (head[0] === 0x1f && head[1] === 0x8b && "DecompressionStream" in window) {
    const plain = new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"));
    bytes = await new Response(plain).arrayBuffer();
  }
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
// On a board with its own messenger: its account. Elsewhere (or on a board
// without one): this browser's own account in WebAssembly.
async function main() {
  if (location.pathname.startsWith("/app/")) {
    const r = await fetch("/api/acct/me").catch(() => null);
    if (r && r.status === 401) return showLogin();
    if (r && r.ok) return startBoard();
  }
  return load();
}

main().catch((e) => {
  $("main").innerHTML = `<div class="card">بارگذاری نشد: ${esc(e.message)}<div class="row"><button class="btn ghost" onclick="location.reload()">دوباره</button></div></div>`;
});
