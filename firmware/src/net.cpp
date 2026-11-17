#include "net.h"

#include <Arduino.h>
#include <DNSServer.h>
#include <HTTPClient.h>
#include <WebServer.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <freertos/FreeRTOS.h>
#include <freertos/semphr.h>
#include <freertos/task.h>
#include <LittleFS.h>
#include <mbedtls/base64.h>
#include <mbedtls/sha256.h>
#include <sys/time.h>
#include <time.h>

#include <cstring>

#include "board.h"
#include "meshtastic.h"
#include "roots.h"

namespace net {
namespace {

using mesh::route::Millis;

constexpr const char* kSettingsFile = "/net";
constexpr const char* kCursorFile = "/cur";
constexpr Millis kDebounce = 60;
constexpr Millis kLeaveSetupMs = 5000;  // after a panel save: let the page reach the phone
constexpr int kPollWait = 10;        // s, gateway long-poll
constexpr size_t kUploadMax = 16;    // packets per call
constexpr size_t kServeMax = 32;
constexpr uint32_t kCursorSaveMs = 5 * 60 * 1000;

Settings* g_s = nullptr;
mesh::node::Node* g_node = nullptr;
SemaphoreHandle_t g_lock = nullptr;
Hooks g_hooks;

Mode g_mode = Mode::Off;
bool g_switch = false, g_switch_raw = false;
Millis g_switch_since = 0;
bool g_forced = false;   // a host asked for the hotspot (HOTSPOT 1), until HOTSPOT 0 or a reboot
Millis g_saved_at = 0;   // the panel saved settings (0 = not recently)
bool g_time_given = false;

DNSServer g_dns;
WebServer g_web(80);
char g_token[33] = {};  // panel session
uint8_t g_login_fail = 0;
Millis g_login_lock_until = 0;
char g_ap_name[24] = {};

volatile bool g_wifi_up = false;
volatile const char* g_relay_state = "off";
volatile uint32_t g_relay_ok = 0, g_relay_fail = 0, g_relay_in = 0, g_relay_up = 0;

struct Lock {
  Lock() { xSemaphoreTake(g_lock, portMAX_DELAY); }
  ~Lock() { xSemaphoreGive(g_lock); }
};

void copy(char* dst, size_t cap, const String& v) {
  size_t n = v.length() < cap - 1 ? v.length() : cap - 1;
  memcpy(dst, v.c_str(), n);
  dst[n] = 0;
}

String html_escape(const char* s) {
  String o;
  for (; *s; s++) {
    switch (*s) {
      case '<': o += "&lt;"; break;
      case '>': o += "&gt;"; break;
      case '&': o += "&amp;"; break;
      case '"': o += "&quot;"; break;
      default: o += *s;
    }
  }
  return o;
}

String hex(const uint8_t* b, size_t n) {
  static const char* d = "0123456789abcdef";
  String o;
  for (size_t i = 0; i < n; i++) {
    o += d[b[i] >> 4];
    o += d[b[i] & 15];
  }
  return o;
}

bool parse_hex(const String& s, uint8_t* out, size_t n) {
  if (s.length() != n * 2) return false;
  for (size_t i = 0; i < n; i++) {
    char hi = s[2 * i], lo = s[2 * i + 1];
    auto v = [](char c) -> int {
      if (c >= '0' && c <= '9') return c - '0';
      if (c >= 'a' && c <= 'f') return c - 'a' + 10;
      if (c >= 'A' && c <= 'F') return c - 'A' + 10;
      return -1;
    };
    if (v(hi) < 0 || v(lo) < 0) return false;
    out[i] = uint8_t(v(hi) << 4 | v(lo));
  }
  return true;
}

// ---- modes ----

void stop_setup() {
  g_dns.stop();
  g_web.stop();
  WiFi.softAPdisconnect(true);
  g_token[0] = 0;
}

void start_setup() {
  WiFi.disconnect(true);
  WiFi.mode(WIFI_AP_STA);  // STA half only scans
  WiFi.softAPConfig(IPAddress(192, 168, 4, 1), IPAddress(192, 168, 4, 1), IPAddress(255, 255, 255, 0));
  WiFi.softAP(g_ap_name, g_s->password);
  g_dns.setErrorReplyCode(DNSReplyCode::NoError);
  g_dns.start(53, "*", WiFi.softAPIP());
  g_web.begin();
  WiFi.scanNetworks(true);
}

void start_station() {
  if (!g_s->wifi_ssid[0]) {
    WiFi.mode(WIFI_OFF);
    return;
  }
  // Joined to the home Wi-Fi and, at the same time, the board's own hotspot
  // (same name and device password): a phone that carries the board in a
  // backpack joins it directly and uses the board's account (no panel here).
  WiFi.mode(WIFI_AP_STA);
  WiFi.softAPConfig(IPAddress(192, 168, 4, 1), IPAddress(192, 168, 4, 1), IPAddress(255, 255, 255, 0));
  WiFi.softAP(g_ap_name, g_s->password);
  WiFi.setAutoReconnect(true);
  WiFi.begin(g_s->wifi_ssid, g_s->wifi_pass);
  configTime(0, 0, "pool.ntp.org", "time.google.com");
  g_web.begin();  // the web app for browsers on this network
}

// The hotspot is on while the setup switch is on, while a host asked for it,
// and while no Wi-Fi is saved (a new board, or one without the switch): there
// is nothing else to join, and the panel or a host is the only way to set one.
Mode wanted_mode() { return g_switch || g_forced || !g_s->wifi_ssid[0] ? Mode::Setup : Mode::Station; }

void set_mode(Mode m) {
  if (m == g_mode) return;
  if (g_mode == Mode::Setup) stop_setup();
  if (g_mode == Mode::Station) {
    g_web.stop();
    WiFi.softAPdisconnect(true);
    WiFi.disconnect(true);
    g_wifi_up = false;
  }
  g_mode = m;
  if (m == Mode::Setup) start_setup();
  if (m == Mode::Station) start_station();
  if (m == Mode::Off) WiFi.mode(WIFI_OFF);
}

// ---- panel ----

// Setup panel style: the Mava design system (docs/design.md) — dark violet,
// neon glow on focus and the primary button, chamfered corners, 48 px
// controls, visible labels. Fonts come from the web client when installed.
const char kStyle[] =
    "<meta name=viewport content='width=device-width,initial-scale=1,viewport-fit=cover'><meta charset=utf-8>"
    "<meta name=theme-color content='#0B0A14'><style>"
    "@font-face{font-family:Vazirmatn;src:url(/app/fonts/vazirmatn-arabic-400-normal.woff2) format('woff2');unicode-range:U+0600-06FF,U+200C-200F,U+FB50-FEFF}"
    "@font-face{font-family:Orbitron;font-weight:700;src:url(/app/fonts/orbitron-latin-700-normal.woff2) format('woff2')}"
    ":root{--bg:#0B0A14;--s1:#15122A;--s2:#1E1A3A;--b:#3B2A6B;--fg:#ECE8FF;--mu:#A9A2CF;--p:#7C3AED;--ph:#A78BFA;--a:#D946EF;--ok:#22D3EE;"
    "--ch:polygon(10px 0,100% 0,100% calc(100% - 10px),calc(100% - 10px) 100%,0 100%,0 10px)}"
    "*{box-sizing:border-box}body{font:16px/1.6 Vazirmatn,system-ui,sans-serif;max-width:560px;margin:auto;padding:16px;"
    "background:var(--bg) radial-gradient(120% 50% at 50% -10%,rgba(124,58,237,.25),transparent 60%) fixed;color:var(--fg)}"
    "h1{font:700 22px Orbitron,sans-serif;letter-spacing:.15em;color:var(--ph);text-shadow:0 0 12px rgba(167,139,250,.6)}"
    "fieldset{background:linear-gradient(180deg,var(--s2),var(--s1));border:1px solid var(--b);clip-path:var(--ch);margin:12px 0;padding:14px}"
    "legend{font-weight:bold;color:var(--ph);padding:0 6px}label{display:block;margin-top:10px;font-size:14px;color:var(--mu)}"
    "input,select{width:100%;min-height:48px;padding:10px;font:16px Vazirmatn,system-ui;margin-top:4px;direction:ltr;color:var(--fg);"
    "background:var(--bg);border:1px solid var(--b);clip-path:var(--ch)}"
    "input:focus,select:focus{outline:none;border-color:var(--ph);box-shadow:0 0 0 1px rgba(167,139,250,.4),0 0 16px rgba(124,58,237,.5)}"
    "input[type=checkbox]{min-height:0;width:20px;height:20px;accent-color:var(--p);vertical-align:middle;margin-inline-end:6px}"
    "button{margin-top:14px;min-height:48px;padding:0 28px;font:700 16px Vazirmatn,system-ui;border:0;color:#fff;cursor:pointer;"
    "background:linear-gradient(135deg,var(--p),var(--a));clip-path:var(--ch);box-shadow:0 0 18px rgba(124,58,237,.5)}"
    "button:focus-visible,a:focus-visible{outline:2px solid var(--a);outline-offset:2px}"
    "table{width:100%;font-size:14px;border-collapse:collapse}td{padding:4px 0;border-bottom:1px solid rgba(59,42,107,.6)}"
    "td:last-child{direction:ltr;text-align:left;color:var(--ok);font-family:ui-monospace,monospace}.n{color:var(--mu);font-size:14px}"
    "a{color:var(--ph)}p b{color:var(--a)}"
    "</style>";

bool authed() {
  if (!g_token[0]) return false;
  String c = g_web.header("Cookie");
  String want = String("mava=") + g_token;
  return c.indexOf(want) >= 0;
}

void send_page(const String& body) {
  g_web.sendHeader("Cache-Control", "no-store");
  g_web.send(200, "text/html; charset=utf-8",
             String("<!doctype html><html lang=fa dir=rtl><head><title>Mava</title>") + kStyle + "</head><body>" +
                 body + "</body></html>");
}

void page_login(const char* msg) {
  String b = "<h1>MAVA</h1><p class=n>پنل تنظیم برد — ورود</p>";
  if (msg && *msg) b += String("<p>") + msg + "</p>";
  b += "<form method=post action=/login><fieldset><label>رمز دستگاه<input name=p type=password "
       "autocomplete=current-password autofocus></label><button>ورود</button></fieldset></form>";
  send_page(b);
}

String row(const char* k, const String& v) { return String("<tr><td>") + k + "</td><td>" + v + "</td></tr>"; }

void page_panel(const char* msg) {
  Millis now = board::now_ms();
  mesh::host::Status st = g_node->status(now);
  const mesh::host::NodeConfig& cfg = g_node->config();
  const mesh::PublicKey& pk = g_node->pub();
  char nid[12];
  snprintf(nid, sizeof nid, "!%08lx", (unsigned long)mesh::meshtastic::Backend::node_id_for(pk.x));

  String b = "<h1>MAVA</h1><p class=n>پنل تنظیم برد · ";
  b += html_escape(cfg.name);
  b += "</p>";
  if (msg && *msg) b += String("<p><b>") + msg + "</b></p>";
  b += "<fieldset><legend>وضعیت</legend><table>";
  b += row("شناسه", hex(pk.ed, 8));
  b += row("Meshtastic", nid);
  b += row("LoRa", String(mesh::host::kRegions[cfg.region].name) + " " + String(st.freq_hz / 1e6, 3) + " MHz " +
                       String(st.tx_dbm) + " dBm");
  b += row("کانال", html_escape(g_s->channel));
  b += row("همسایه‌ها / ذخیره", String(st.neighbours) + " / " + String(st.stored));
  b += row("زمان پخش (ساعت)", String(st.air_used_ms / 1000) + " / " + String(st.air_budget_ms / 1000) + " s");
  b += row("USB / BLE", String(g_node->host_attached(0) ? "وصل" : "—") + " / " +
                            (g_node->host_attached(1) ? "وصل" : "—"));
  b += row("رله", String((const char*)g_relay_state) + " ↑" + String(g_relay_up) + " ↓" + String(g_relay_in));
  b += row("فریم‌ور", String("v") + String(MESH_FIRMWARE_VERSION));
  b += "</table></fieldset>";

  b += "<form method=post action=/save>";
  b += "<fieldset><legend>وای‌فای (اینترنت)</legend><label>شبکه<input name=ssid list=nets value=\"" +
       html_escape(g_s->wifi_ssid) + "\"></label><datalist id=nets>";
  int n = WiFi.scanComplete();
  for (int i = 0; i < n && i < 20; i++) b += "<option value=\"" + html_escape(WiFi.SSID(i).c_str()) + "\">";
  b += "</datalist><label>رمز وای‌فای (خالی = بدون تغییر)<input name=wpass type=password></label></fieldset>";

  b += "<fieldset><legend>رله</legend><label>آدرس<input name=relay value=\"" + html_escape(g_s->relay_url) +
       "\"></label><label><input type=checkbox name=relay_on style=width:auto" + (g_s->relay_on ? " checked" : "") +
       "> اتصال به رله</label><label><input type=checkbox name=carrier style=width:auto" +
       (g_s->carrier ? " checked" : "") +
       "> حمل پیام‌های منطقه (carrier)</label><label>سقف carrier (KB در ساعت)<input name=quota type=number value=" +
       String(g_s->quota_kb_h) + "></label></fieldset>";

  b += "<fieldset><legend>LoRa / Meshtastic</legend><label>منطقه<select name=region>";
  for (size_t i = 0; i < mesh::host::kRegionCount; i++)
    b += "<option value=" + String(i) + (i == cfg.region ? " selected>" : ">") + mesh::host::kRegions[i].name +
         "</option>";
  b += "</select></label><label>فرکانس (Hz، صفر = پیش‌فرض منطقه)<input name=freq type=number value=" +
       String(cfg.freq_hz) + "></label><label>توان (dBm)<input name=dbm type=number value=" + String(cfg.tx_dbm) +
       "></label><label>نام کانال<input name=ch maxlength=11 value=\"" + html_escape(g_s->channel) +
       "\"></label><label>کلید کانال: default، none یا hex (۳۲ یا ۶۴ رقم)<input name=psk value=\"" +
       (g_s->psk_len == 1 ? String(g_s->psk[0] == 0 ? "none" : "default") : String("(بدون تغییر)")) +
       "\"></label><label>نام نود<input name=name maxlength=" + String(mesh::host::kMaxNameLen) + " value=\"" +
       html_escape(cfg.name) + "\"></label></fieldset>";

  b += "<fieldset><legend>رمز دستگاه</legend><label>رمز جدید هات‌اسپات و پنل (۸ تا ۱۶ حرف، خالی = بدون "
       "تغییر)<input name=newpass type=password minlength=8 maxlength=16></label></fieldset>";
  b += "<button>ذخیره</button></form>";
  b += "<p class=n>بعد از ذخیره، اگر کلید setup خاموش باشد برد خودش به وای‌فای وصل می‌شود؛ وگرنه کلید را "
       "خاموش کنید.</p>";
  b += "<p><a href=/app/>پیام‌رسان در مرورگر (Mava web)</a></p>";
  send_page(b);
}

bool web_client_installed();
void page_no_client();

void handle_root() {
  if (g_mode != Mode::Setup) {  // on the home Wi-Fi the board serves the web app
    if (!web_client_installed()) return page_no_client();
    g_web.sendHeader("Location", "/app/");
    g_web.send(302);
    return;
  }
  if (!authed()) return page_login(nullptr);
  page_panel(nullptr);
}

// ---- web app link (docs/web-access.md) ----
//
// POST /api/link?s=<session>: the body is base64 host-link bytes from the
// browser; the reply is base64 host-link bytes for it. The browser is host
// port 2 of the node (untrusted: packets and status only). One browser at a
// time; a session that stops polling for 15 s is detached.

constexpr Millis kWebIdleMs = 15000;
String g_web_session;
Millis g_web_last = 0;

void handle_link() {
  Millis now = board::now_ms();
  String s = g_web.arg("s");
  if (s.length() < 8 || s.length() > 32) return g_web.send(400, "text/plain", "session");
  if (g_web_session.length() && s != g_web_session && now - g_web_last < kWebIdleMs)
    return g_web.send(409, "text/plain", "another browser is attached");
  if (s != g_web_session) {
    g_node->host_detach(mesh::node::Node::kWebPort);
    g_web_session = s;
  }
  g_web_last = now;
  const String& body = g_web.arg("plain");
  static uint8_t in[3072], out[4096];
  static char text[5600];
  size_t n = 0;
  if (body.length() && mbedtls_base64_decode(in, sizeof in, &n, reinterpret_cast<const uint8_t*>(body.c_str()),
                                             body.length()) != 0)
    return g_web.send(400, "text/plain", "base64");
  if (n) g_node->host_rx(mesh::node::Node::kWebPort, in, n, now);
  else if (!g_node->host_attached(mesh::node::Node::kWebPort)) g_node->host_attach(mesh::node::Node::kWebPort, now);
  size_t m = g_node->host_tx(mesh::node::Node::kWebPort, out, sizeof out);
  size_t tn = 0;
  mbedtls_base64_encode(reinterpret_cast<uint8_t*>(text), sizeof text, &tn, out, m);
  text[tn] = 0;
  g_web.sendHeader("Cache-Control", "no-store");
  g_web.send(200, "text/plain", text);
}

bool web_client_installed() { return LittleFS.exists("/web/index.htm.gz") || LittleFS.exists("/web/index.htm"); }

// The browser client is missing from the board's file system (a board flashed
// with "Update firmware only" that never had "Install everything", or wiped by
// firmware before beta21): say so instead of a bare 404.
void page_no_client() {
  g_web.sendHeader("Cache-Control", "no-store");
  g_web.send(404, "text/html; charset=utf-8",
             "<!doctype html><meta charset=utf-8><meta name=viewport content='width=device-width'>"
             "<title>Mava</title><body style='font-family:sans-serif;max-width:32em;margin:2em auto;padding:0 1em'>"
             "<h2>Mava</h2><p>The browser app is not installed on this board.</p>"
             "<p>Flash it with Mava Flasher and choose <b>Install everything</b>.</p>"
             "<p dir=rtl>برنامهٔ مرورگر روی این برد نصب نیست. برد را با فلشر Mava و گزینهٔ "
             "<b>نصب همه‌چیز</b> فلش کنید.</p>");
}

void not_found() {
  if (g_mode == Mode::Setup) {
    g_web.sendHeader("Location", String("http://") + WiFi.softAPIP().toString() + "/");
    g_web.send(302);
  } else if (g_web.uri() == "/app" || g_web.uri().startsWith("/app/")) {
    if (!web_client_installed()) return page_no_client();
    if (g_web.uri() == "/app") {
      g_web.sendHeader("Location", "/app/");
      return g_web.send(302);
    }
    g_web.send(404, "text/plain", "not found");
  } else {
    // Any other address on the home Wi-Fi: the web app.
    g_web.sendHeader("Location", "/app/");
    g_web.send(302);
  }
}

void handle_login() {
  Millis now = board::now_ms();
  if (now < g_login_lock_until) return page_login("چند دقیقه بعد دوباره امتحان کنید.");
  if (g_web.arg("p") != g_s->password) {
    if (++g_login_fail >= 5) {
      g_login_fail = 0;
      g_login_lock_until = now + 60000;
    }
    return page_login("رمز اشتباه است.");
  }
  g_login_fail = 0;
  uint8_t t[16];
  board::random_bytes(t, sizeof t);
  copy(g_token, sizeof g_token, hex(t, sizeof t));
  g_web.sendHeader("Set-Cookie", String("mava=") + g_token + "; Path=/; HttpOnly; SameSite=Strict");
  g_web.sendHeader("Location", "/");
  g_web.send(303);
}

void acct_logout_all();

void handle_save() {
  if (!authed()) return page_login(nullptr);
  Settings s = *g_s;
  String err;
  copy(s.wifi_ssid, sizeof s.wifi_ssid, g_web.arg("ssid"));
  if (g_web.arg("wpass").length()) copy(s.wifi_pass, sizeof s.wifi_pass, g_web.arg("wpass"));
  String relay = g_web.arg("relay");
  relay.trim();
  while (relay.endsWith("/")) relay.remove(relay.length() - 1);
  if (relay.length() && !relay.startsWith("https://") && !relay.startsWith("http://")) err = "آدرس رله باید با https:// شروع شود.";
  copy(s.relay_url, sizeof s.relay_url, relay);
  s.relay_on = g_web.hasArg("relay_on");
  s.carrier = g_web.hasArg("carrier");
  s.quota_kb_h = uint16_t(constrain(g_web.arg("quota").toInt(), 0, 60000));

  mesh::meshtastic::Channel ch;
  copy(ch.name, sizeof ch.name, g_web.arg("ch"));
  String psk = g_web.arg("psk");
  psk.trim();
  if (psk == "default") {
    ch.psk_len = 1;
    ch.psk[0] = 1;
  } else if (psk == "none") {
    ch.psk_len = 1;
    ch.psk[0] = 0;
  } else if (parse_hex(psk, ch.psk, 16)) {
    ch.psk_len = 16;
  } else if (parse_hex(psk, ch.psk, 32)) {
    ch.psk_len = 32;
  } else {
    memcpy(ch.psk, s.psk, sizeof ch.psk);  // unchanged
    ch.psk_len = s.psk_len;
  }
  if (!ch.valid()) err = "کانال نامعتبر است.";

  String np = g_web.arg("newpass");
  if (np.length() && (np.length() < 8 || np.length() > 16)) err = "رمز دستگاه باید ۸ تا ۱۶ حرف باشد.";

  mesh::host::NodeConfig c = g_node->config();
  c.region = uint8_t(g_web.arg("region").toInt());
  c.freq_hz = uint32_t(g_web.arg("freq").toInt());
  c.tx_dbm = int8_t(g_web.arg("dbm").toInt());
  copy(c.name, sizeof c.name, g_web.arg("name"));
  if (!c.valid()) err = "منطقه، فرکانس یا توان برای این منطقه مجاز نیست.";

  if (err.length()) return page_panel(err.c_str());

  bool ch_changed = strcmp(ch.name, s.channel) != 0 || ch.psk_len != s.psk_len || memcmp(ch.psk, s.psk, ch.psk_len) != 0;
  memcpy(s.channel, ch.name, sizeof s.channel);
  memcpy(s.psk, ch.psk, sizeof s.psk);
  s.psk_len = ch.psk_len;
  bool pass_changed = np.length() > 0;
  if (pass_changed) {
    copy(s.password, sizeof s.password, np);
    acct_logout_all();  // browsers log in again with the new password
  }
  *g_s = s;
  save(s);
  g_node->apply_config(c, board::now_ms());
  if (ch_changed && g_hooks.channel_changed) g_hooks.channel_changed(s);
  if (pass_changed) {
    // New hotspot password: restart the hotspot (the phone has to rejoin).
    page_panel("ذخیره شد. هات‌اسپات با رمز جدید دوباره راه‌اندازی می‌شود.");
    delay(300);
    stop_setup();
    start_setup();
    return;
  }
  g_saved_at = board::now_ms() | 1;
  if (wanted_mode() == Mode::Station) {
    page_panel("ذخیره شد. چند ثانیهٔ دیگر هات‌اسپات خاموش می‌شود و برد به وای‌فای وصل می‌شود. "
               "برای بازگشت به این پنل، کلید setup را روشن کنید.");
  } else {
    page_panel("ذخیره شد. برای اتصال به وای‌فای، کلید setup را خاموش کنید.");
  }
}

// The web client's WebAssembly: Arduino's static handler doesn't know .wasm,
// sends it as application/octet-stream and then leaves out
// "Content-Encoding: gzip", so the browser got the gzip bytes. Served here as
// application/wasm; streamFile adds the encoding for a .gz file.
void serve_wasm() {
  File f = LittleFS.open("/web/main.wasm.gz", "r");
  if (!f) {
    g_web.send(404, "text/plain", "web client not installed");
    return;
  }
  g_web.sendHeader("Cache-Control", "max-age=600");
  g_web.streamFile(f, "application/wasm");
  f.close();
}

// ---- the account API for the browser app (one account on the device) ----

Account* g_account = nullptr;
// Browser sessions logged in with the device password (until reboot).
constexpr int kSessions = 4;
char g_sessions[kSessions][33] = {};
int g_session_next = 0;

bool acct_authed() {
  String c = g_web.header("Cookie");
  for (auto& t : g_sessions)
    if (t[0] && c.indexOf(String("mavaacct=") + t) >= 0) return true;
  return false;
}

void acct_logout_all() { memset(g_sessions, 0, sizeof g_sessions); }

void json(int code, const std::string& body) {
  g_web.sendHeader("Cache-Control", "no-store");
  g_web.send(code, "application/json; charset=utf-8", body.c_str());
}

// Common checks: 404 without a messenger on this board (the page then uses
// its own in-browser account), 401 before login.
bool acct_ready() {
  if (!g_account) {
    json(404, "{\"error\":\"no messenger on this board\"}");
    return false;
  }
  if (!acct_authed()) {
    json(401, "{\"error\":\"login\"}");
    return false;
  }
  return true;
}

void acct_login() {
  if (!g_account) return json(404, "{\"error\":\"no messenger on this board\"}");
  Millis now = board::now_ms();
  if (now < g_login_lock_until) return json(429, "{\"error\":\"wait\"}");
  if (g_web.arg("password") != g_s->password) {
    if (++g_login_fail >= 5) {
      g_login_fail = 0;
      g_login_lock_until = now + 60000;
    }
    return json(403, "{\"error\":\"password\"}");
  }
  g_login_fail = 0;
  uint8_t t[16];
  board::random_bytes(t, sizeof t);
  char* slot = g_sessions[g_session_next++ % kSessions];
  copy(slot, 33, hex(t, sizeof t));
  // A year: the phone stays logged in; a reboot or a new password ends it.
  g_web.sendHeader("Set-Cookie", String("mavaacct=") + slot + "; Path=/; Max-Age=31536000; HttpOnly; SameSite=Strict");
  json(200, g_account->me());
}

void redirect_portal() {
  g_web.sendHeader("Location", String("http://") + WiFi.softAPIP().toString() + "/");
  g_web.send(302);
}

// ---- relay worker ----

String g_cursor_saved;
int64_t g_cursor = 0;
int g_pow_bits = -1;
// The relay address in use: the configured one, or for the default relay its
// IP address when the name doesn't work (blocked or filtered DNS, TLS).
constexpr const char* kDefaultRelay = "https://eu-1.mfadem.ir";
constexpr const char* kDefaultRelayIp = "http://87.106.5.192";
String g_base;
int g_fails = 0;  // gateway calls failed in a row: look for the relay again after a few
// Why the relay is not reachable, for the log and the panel (two buffers, so a
// reader never sees one half-written).
char g_detail[2][120];
int g_detail_i = 0;

void set_detail(const char* state, const String& why) {
  int n = g_detail_i ^ 1;
  snprintf(g_detail[n], sizeof g_detail[n], "%s (%s)", state, why.c_str());
  g_detail_i = n;
  g_relay_state = g_detail[n];
}
uint32_t g_cursor_saved_at = 0;

// Parses the relay's frame stream (len u32 BE || packet) as it arrives and
// hands each packet to the node.
class FrameSink : public Stream {
 public:
  size_t write(uint8_t b) override { return write(&b, 1); }
  size_t write(const uint8_t* p, size_t n) override {
    for (size_t i = 0; i < n; i++) {
      if (hdr_ < 4) {
        len_ = len_ << 8 | p[i];
        if (++hdr_ == 4) {
          got_ = 0;
          if (len_ == 0) hdr_ = len_ = 0;
        }
        continue;
      }
      if (got_ < sizeof buf_) buf_[got_] = p[i];
      if (++got_ == len_) {
        if (len_ <= sizeof buf_) {
          Lock l;
          if (g_node->uplink_rx(buf_, len_, board::now_ms())) g_relay_in = g_relay_in + 1;
        }
        hdr_ = 0;
        len_ = 0;
      }
    }
    return n;
  }
  int available() override { return 0; }
  int read() override { return -1; }
  int peek() override { return -1; }
  void flush() override {}

 private:
  uint8_t buf_[mesh::kLoRaMTU];
  uint32_t len_ = 0, got_ = 0;
  uint8_t hdr_ = 0;
};

uint64_t solve_pow(const uint8_t* body, size_t n, int bits) {
  mbedtls_sha256_context base, c;
  mbedtls_sha256_init(&base);
  mbedtls_sha256_starts(&base, 0);
  mbedtls_sha256_update(&base, reinterpret_cast<const uint8_t*>("MESHMSG-POW"), 11);
  mbedtls_sha256_update(&base, body, n);
  uint8_t d[32];
  for (uint64_t nonce = 0;; nonce++) {
    uint8_t nb[8];
    for (int i = 0; i < 8; i++) nb[i] = uint8_t(nonce >> (56 - 8 * i));
    mbedtls_sha256_init(&c);
    mbedtls_sha256_clone(&c, &base);
    mbedtls_sha256_update(&c, nb, 8);
    mbedtls_sha256_finish(&c, d);
    mbedtls_sha256_free(&c);
    int z = 0;
    for (int i = 0; i < 32 && z == 8 * i; i++) z += d[i] ? __builtin_clz(uint32_t(d[i])) - 24 : 8;
    if (z >= bits) {
      mbedtls_sha256_free(&base);
      return nonce;
    }
    if ((nonce & 4095) == 4095) vTaskDelay(1);
  }
}

// "Tue, 17 Nov 2026 11:40:00 GMT" → Unix seconds (0 if not that form).
uint32_t http_date(const String& d) {
  static const char* kMon = "JanFebMarAprMayJunJulAugSepOctNovDec";
  int day, year, h, m, s;
  char mon[4] = {};
  if (sscanf(d.c_str(), "%*3s, %d %3s %d %d:%d:%d", &day, mon, &year, &h, &m, &s) != 6) return 0;
  const char* p = strstr(kMon, mon);
  if (!p || year < 2024) return 0;
  int mo = int(p - kMon) / 3 + 1;
  // Days from 1970-01-01 (civil calendar).
  int y = year - (mo <= 2);
  int era = y / 400, yoe = y - era * 400;
  int doy = (153 * (mo + (mo > 2 ? -3 : 9)) + 2) / 5 + day - 1;
  int doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
  long days = long(era) * 146097 + doe - 719468;
  return uint32_t(days * 86400L + h * 3600L + m * 60L + s);
}

// Without NTP (filtered on some networks) the relay's clock is good enough
// for announces: the board's account can then announce and be reached.
void clock_from(const String& date) {
  if (g_time_given || !date.length()) return;
  uint32_t t = http_date(date);
  if (!t) return;
  struct timeval tv = {time_t(t), 0};
  settimeofday(&tv, nullptr);
  Lock l;
  g_node->set_time(t, board::now_ms());
  g_time_given = true;
}

bool begin_http(HTTPClient& http, WiFiClientSecure& tls, WiFiClient& plain, const String& url) {
  if (url.startsWith("https://")) {
    tls.setCACert(kRootCAs);
    return http.begin(tls, url);
  }
  return http.begin(plain, url);
}

// Asks one relay address for /v1/info; on failure why holds the reason.
bool probe(const String& base, String& why) {
  String host = base.substring(base.indexOf("//") + 2);
  int cut = host.indexOf('/');
  if (cut >= 0) host = host.substring(0, cut);
  cut = host.indexOf(':');
  if (cut >= 0) host = host.substring(0, cut);
  IPAddress ip;
  if (!WiFi.hostByName(host.c_str(), ip)) {
    why = host + ": name lookup failed";
    return false;
  }
  HTTPClient http;
  WiFiClientSecure tls;
  WiFiClient plain;
  if (!begin_http(http, tls, plain, base + "/v1/info")) {
    why = base + ": bad address";
    return false;
  }
  http.setTimeout(15000);
  const char* keep[] = {"Date"};
  http.collectHeaders(keep, 1);
  int code = http.GET();
  if (code > 0) clock_from(http.header("Date"));
  if (code != 200) {
    if (code < 0) {
      char err[80] = {};
      if (base.startsWith("https://")) tls.lastError(err, sizeof err);
      why = base + ": " + HTTPClient::errorToString(code) + (err[0] ? String(", TLS: ") + err : String());
    } else {
      why = base + ": HTTP " + String(code);
    }
    http.end();
    return false;
  }
  String b = http.getString();
  http.end();
  int i = b.indexOf("\"pow_bits\":");
  g_pow_bits = i < 0 ? 0 : b.substring(i + 11).toInt();
  return true;
}

// Finds a relay address that answers: the configured one, then (for the
// default relay) its IP address. Sets g_base.
bool fetch_info() {
  String first = String(g_s->relay_url), why1, why2;
  if (probe(first, why1)) {
    g_base = first;
    return true;
  }
  if (first == kDefaultRelay && probe(kDefaultRelayIp, why2)) {
    g_base = kDefaultRelayIp;
    return true;
  }
  set_detail("unreachable", why2.length() ? why1 + "; " + why2 : why1);
  return false;
}

void load_cursor() {
  int64_t c = 0;
  if (board::storage_read(kCursorFile, &c, sizeof c) == sizeof c && c > 0) g_cursor = c;
}

void save_cursor(bool force) {
  uint32_t now = millis();
  if (!force && now - g_cursor_saved_at < kCursorSaveMs) return;
  g_cursor_saved_at = now;
  board::storage_write(kCursorFile, &g_cursor, sizeof g_cursor);
}

// One gateway call: upload what the relay lacks, fetch what is for us.
bool gateway_once(uint8_t* body, size_t cap) {
  uint32_t seqs[kUploadMax];
  size_t k = 0, used = 0;
  {
    Lock l;
    uint8_t served[kServeMax][16];
    size_t s = g_node->served(served, kServeMax);
    body[0] = uint8_t(s >> 8);
    body[1] = uint8_t(s);
    memcpy(body + 2, served, s * 16);
    used = 2 + s * 16;
    used += g_node->uplink_take(body + used, cap - used, seqs, kUploadMax, &k);
  }
  auto give_back = [&]() {
    if (!k) return;
    Lock l;
    g_node->uplink_return(seqs, k);
  };

  char q[96];
  uint32_t quota = g_s->carrier ? uint32_t(g_s->quota_kb_h) * 1024 / 360 : 0;  // per ~10 s poll
  snprintf(q, sizeof q, "/v1/gateway?after=%lld&wait=%d&quota=%lu%s", (long long)g_cursor, k ? 0 : kPollWait,
           (unsigned long)quota, g_s->carrier ? "&carrier=1" : "");
  HTTPClient http;
  WiFiClientSecure tls;
  WiFiClient plain;
  if (!begin_http(http, tls, plain, g_base + q)) {
    give_back();
    return false;
  }
  http.setTimeout(uint16_t((kPollWait + 20) * 1000));
  const char* keep[] = {"X-Cursor", "Date"};
  http.collectHeaders(keep, 2);
  http.addHeader("Content-Type", "application/x-meshmsg-frames");
  char origin[24];
  snprintf(origin, sizeof origin, "mava-%02x%02x%02x%02x", g_node->pub().ed[0], g_node->pub().ed[1],
           g_node->pub().ed[2], g_node->pub().ed[3]);
  http.addHeader("X-Origin", origin);
  if (k && g_pow_bits > 0) http.addHeader("X-PoW", String((unsigned long long)solve_pow(body, used, g_pow_bits)));
  int code = http.POST(body, used);
  if (code != 200) {
    http.end();
    give_back();
    if (code == 402) g_pow_bits = -1;  // the relay raised its proof of work: ask again
    set_detail("error", g_base + ": " + (code < 0 ? HTTPClient::errorToString(code) : "HTTP " + String(code)));
    return false;
  }
  g_relay_up = g_relay_up + uint32_t(k);
  FrameSink sink;
  http.writeToStream(&sink);
  clock_from(http.header("Date"));
  String cur = http.header("X-Cursor");
  http.end();
  if (cur.length()) {
    int64_t c = atoll(cur.c_str());
    if (c != g_cursor) {
      g_cursor = c;
      save_cursor(false);
    }
  }
  return true;
}

void relay_task(void*) {
  size_t cap = 2 + kServeMax * 16 + kUploadMax * (4 + mesh::kLoRaMTU);
  uint8_t* body = static_cast<uint8_t*>(malloc(cap));
  uint32_t backoff = 2000;
  load_cursor();
  for (;;) {
    bool up = g_mode == Mode::Station && WiFi.status() == WL_CONNECTED;
    g_wifi_up = up;
    if (!up || !g_s->relay_on || !g_s->relay_url[0] || !body) {
      g_relay_state = g_mode == Mode::Setup ? "setup" : (g_s->wifi_ssid[0] ? "no wifi" : "off");
      vTaskDelay(pdMS_TO_TICKS(1000));
      continue;
    }
    if (g_pow_bits < 0 && !fetch_info()) {
      g_relay_fail = g_relay_fail + 1;
      vTaskDelay(pdMS_TO_TICKS(backoff));
      backoff = backoff < 60000 ? backoff * 2 : 60000;
      continue;
    }
    if (gateway_once(body, cap)) {
      g_relay_state = g_base == String(g_s->relay_url) ? "online" : "online (by IP address)";
      g_relay_ok = g_relay_ok + 1;
      g_fails = 0;
      backoff = 2000;
    } else {
      if (++g_fails >= 3) g_pow_bits = -1;  // look for the relay again (maybe by IP)
      g_relay_fail = g_relay_fail + 1;
      vTaskDelay(pdMS_TO_TICKS(backoff));
      backoff = backoff < 60000 ? backoff * 2 : 60000;
    }
  }
}

}  // namespace

bool load(Settings& s) {
  Settings t;
  if (board::storage_read(kSettingsFile, &t, sizeof t) != sizeof t || t.magic != kSettingsMagic) return false;
  t.wifi_ssid[sizeof t.wifi_ssid - 1] = 0;
  t.wifi_pass[sizeof t.wifi_pass - 1] = 0;
  t.relay_url[sizeof t.relay_url - 1] = 0;
  t.password[sizeof t.password - 1] = 0;
  t.channel[sizeof t.channel - 1] = 0;
  s = t;
  return true;
}

bool save(const Settings& s) { return board::storage_write(kSettingsFile, &s, sizeof s); }

void init_secrets(Settings& s) {
  // Every board starts with the same device password (owner's choice): set a
  // new one in the setup panel, which is kept from then on. A full erase or
  // "Install everything" brings back the default.
  if (strlen(s.password) < 8) copy(s.password, sizeof s.password, kDefaultPassword);
  if (s.ble_passkey < 100000 || s.ble_passkey > 999999) s.ble_passkey = 100000 + board::random32() % 900000;
}

bool begin(Settings& s, mesh::node::Node& node, void* node_lock, const Hooks& hooks) {
  g_s = &s;
  g_node = &node;
  g_lock = static_cast<SemaphoreHandle_t>(node_lock);
  g_hooks = hooks;
  snprintf(g_ap_name, sizeof g_ap_name, "Mava-%02X%02X", node.pub().ed[0], node.pub().ed[1]);
  WiFi.persistent(false);
  WiFi.mode(WIFI_OFF);

  const char* hdrs[] = {"Cookie"};
  g_web.collectHeaders(hdrs, 1);
  g_web.on("/", HTTP_GET, handle_root);
  g_web.on("/login", HTTP_POST, handle_login);
  g_web.on("/save", HTTP_POST, handle_save);
  // Captive-portal probes of Android, iOS/macOS and Windows: send them to the panel.
  g_web.on("/generate_204", redirect_portal);
  g_web.on("/gen_204", redirect_portal);
  g_web.on("/hotspot-detect.html", redirect_portal);
  g_web.on("/connecttest.txt", redirect_portal);
  g_web.on("/ncsi.txt", redirect_portal);
  g_web.on("/api/link", HTTP_POST, handle_link);
  g_web.on("/api/acct/login", HTTP_POST, acct_login);
  g_web.on("/api/acct/me", HTTP_GET, [] {
    if (acct_ready()) json(200, g_account->me());
  });
  g_web.on("/api/acct/chats", HTTP_GET, [] {
    if (acct_ready()) json(200, g_account->chats());
  });
  g_web.on("/api/acct/messages", HTTP_GET, [] {
    if (acct_ready()) json(200, g_account->messages(g_web.arg("h").c_str()));
  });
  g_web.on("/api/acct/rev", HTTP_GET, [] {
    if (acct_ready()) json(200, "{\"rev\":" + std::to_string(g_account->revision()) + "}");
  });
  g_web.on("/api/acct/send", HTTP_POST, [] {
    if (!acct_ready()) return;
    if (g_account->send(g_web.arg("h").c_str(), g_web.arg("text").c_str())) json(200, "{}");
    else json(400, "{\"error\":\"not sent\"}");
  });
  g_web.on("/api/acct/add", HTTP_POST, [] {
    if (!acct_ready()) return;
    std::string c = g_account->add(g_web.arg("link").c_str(), g_web.arg("name").c_str());
    if (c.empty()) json(400, "{\"error\":\"bad link\"}");
    else json(200, c);
  });
  g_web.on("/api/acct/name", HTTP_POST, [] {
    if (!acct_ready()) return;
    g_account->set_name(g_web.arg("name").c_str());
    json(200, g_account->me());
  });
  g_web.on("/app/main.wasm", HTTP_GET, serve_wasm);
  g_web.serveStatic("/app/", LittleFS, "/web/", "max-age=600");  // index.htm, *.gz
  g_web.onNotFound(not_found);

  g_switch = g_switch_raw = board::setup_switch_on();
  g_switch_since = board::now_ms();
  set_mode(wanted_mode());
  return xTaskCreatePinnedToCore(relay_task, "mava-relay", 12288, nullptr, 1, nullptr, 0) == pdPASS;
}

void loop(Millis now) {
  bool raw = board::setup_switch_on();
  if (raw != g_switch_raw) {
    g_switch_raw = raw;
    g_switch_since = now;
  } else if (raw != g_switch && now - g_switch_since >= kDebounce) {
    g_switch = raw;
    g_saved_at = 0;
  }
  Mode want = wanted_mode();
  if (want != g_mode && !(g_mode == Mode::Setup && g_saved_at && now - g_saved_at < kLeaveSetupMs)) {
    g_saved_at = 0;
    set_mode(want);
  }
  if (g_mode == Mode::Setup) g_dns.processNextRequest();
  if (g_mode != Mode::Off) g_web.handleClient();
  if (g_web_session.length() && now - g_web_last > kWebIdleMs) {
    g_node->host_detach(mesh::node::Node::kWebPort);
    g_web_session = "";
  }
  if (g_mode == Mode::Station && !g_time_given) {
    time_t t = time(nullptr);
    if (t > 1700000000) {
      g_node->set_time(uint32_t(t), now);
      g_time_given = true;
    }
  }
}

uint8_t set_wifi(const mesh::host::WifiConfig& w) {
  Settings s = *g_s;
  if (w.has_ssid) copy(s.wifi_ssid, sizeof s.wifi_ssid, w.ssid);
  if (w.has_pass) copy(s.wifi_pass, sizeof s.wifi_pass, w.pass);
  if (w.has_relay) copy(s.relay_url, sizeof s.relay_url, w.relay);
  bool changed = strcmp(s.wifi_ssid, g_s->wifi_ssid) != 0 || strcmp(s.wifi_pass, g_s->wifi_pass) != 0;
  *g_s = s;
  save(s);
  // Joined with the old network: join again with the new one (loop() starts it).
  if (changed && g_mode == Mode::Station) set_mode(Mode::Off);
  g_saved_at = 0;
  return mesh::host::kAckOk;
}

uint8_t hotspot(bool on) {
  g_forced = on;
  g_saved_at = 0;
  return mesh::host::kAckOk;
}

const char* ap_name() { return g_ap_name; }

void set_account(Account* a) { g_account = a; }

Mode mode() { return g_mode; }
bool wifi_up() { return g_wifi_up; }
const char* relay_state() { return const_cast<const char*>(g_relay_state); }

const char* web_url() {
  static char url[40];
  if (g_mode == Mode::Setup) {
    snprintf(url, sizeof url, "http://%s/", WiFi.softAPIP().toString().c_str());
  } else if (g_mode == Mode::Station && WiFi.status() == WL_CONNECTED) {
    snprintf(url, sizeof url, "http://%s/", WiFi.localIP().toString().c_str());
  } else {
    url[0] = 0;
  }
  return url;
}

}  // namespace net
