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
#include <mbedtls/sha256.h>
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
  WiFi.mode(WIFI_STA);
  WiFi.setAutoReconnect(true);
  WiFi.begin(g_s->wifi_ssid, g_s->wifi_pass);
  configTime(0, 0, "pool.ntp.org", "time.google.com");
}

void set_mode(Mode m) {
  if (m == g_mode) return;
  if (g_mode == Mode::Setup) stop_setup();
  if (g_mode == Mode::Station) {
    WiFi.disconnect(true);
    g_wifi_up = false;
  }
  g_mode = m;
  if (m == Mode::Setup) start_setup();
  if (m == Mode::Station) start_station();
  if (m == Mode::Off) WiFi.mode(WIFI_OFF);
}

// ---- panel ----

const char kStyle[] =
    "<meta name=viewport content='width=device-width,initial-scale=1'><meta charset=utf-8>"
    "<style>body{font-family:sans-serif;max-width:560px;margin:auto;padding:12px;background:#f5f6f8;color:#16181d}"
    "h1{font-size:20px}fieldset{background:#fff;border:1px solid #dde;border-radius:10px;margin:12px 0;padding:10px}"
    "legend{font-weight:bold}label{display:block;margin-top:8px;font-size:14px}"
    "input,select{width:100%;box-sizing:border-box;padding:8px;font-size:15px;margin-top:3px;direction:ltr}"
    "button{margin-top:12px;padding:10px 18px;font-size:16px;border:0;border-radius:8px;background:#2a7de1;color:#fff}"
    "table{width:100%;font-size:13px}td:last-child{direction:ltr;text-align:left}.n{color:#666;font-size:13px}"
    "@media(prefers-color-scheme:dark){body{background:#15171b;color:#e7e9ee}fieldset{background:#1e2127;border-color:#333}}"
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
  String b = "<h1>Mava — ورود به پنل</h1>";
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

  String b = "<h1>Mava — ";
  b += html_escape(cfg.name);
  b += "</h1>";
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
  b += row("رمز جفت‌شدن BLE", String(g_s->ble_passkey));
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
  b += "<p class=n>بعد از ذخیره، کلید setup را خاموش کنید تا برد به وای‌فای وصل شود.</p>";
  send_page(b);
}

void handle_root() {
  if (!authed()) return page_login(nullptr);
  page_panel(nullptr);
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
  if (pass_changed) copy(s.password, sizeof s.password, np);
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
  page_panel("ذخیره شد. برای اتصال به وای‌فای، کلید setup را خاموش کنید.");
}

void redirect_portal() {
  g_web.sendHeader("Location", String("http://") + WiFi.softAPIP().toString() + "/");
  g_web.send(302);
}

// ---- relay worker ----

String g_cursor_saved;
int64_t g_cursor = 0;
int g_pow_bits = -1;
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

bool begin_http(HTTPClient& http, WiFiClientSecure& tls, WiFiClient& plain, const String& url) {
  if (url.startsWith("https://")) {
    tls.setCACert(kRootCAs);
    return http.begin(tls, url);
  }
  return http.begin(plain, url);
}

bool fetch_info() {
  HTTPClient http;
  WiFiClientSecure tls;
  WiFiClient plain;
  if (!begin_http(http, tls, plain, String(g_s->relay_url) + "/v1/info")) return false;
  http.setTimeout(15000);
  int code = http.GET();
  if (code != 200) {
    http.end();
    return false;
  }
  String b = http.getString();
  http.end();
  int i = b.indexOf("\"pow_bits\":");
  g_pow_bits = i < 0 ? 0 : b.substring(i + 11).toInt();
  return true;
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
  if (!begin_http(http, tls, plain, String(g_s->relay_url) + q)) {
    give_back();
    return false;
  }
  http.setTimeout(uint16_t((kPollWait + 20) * 1000));
  const char* keep[] = {"X-Cursor"};
  http.collectHeaders(keep, 1);
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
    return false;
  }
  g_relay_up = g_relay_up + uint32_t(k);
  FrameSink sink;
  http.writeToStream(&sink);
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
      g_relay_state = "unreachable";
      g_relay_fail = g_relay_fail + 1;
      vTaskDelay(pdMS_TO_TICKS(backoff));
      backoff = backoff < 60000 ? backoff * 2 : 60000;
      continue;
    }
    if (gateway_once(body, cap)) {
      g_relay_state = "online";
      g_relay_ok = g_relay_ok + 1;
      backoff = 2000;
    } else {
      g_relay_state = "error";
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
  // No look-alike characters: it is read off a label or a log.
  static const char kAlphabet[] = "abcdefghjkmnpqrstuvwxyz23456789";
  if (strlen(s.password) < 8) {
    for (int i = 0; i < 10; i++) s.password[i] = kAlphabet[board::random32() % (sizeof kAlphabet - 1)];
    s.password[10] = 0;
  }
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
  g_web.onNotFound(redirect_portal);

  g_switch = g_switch_raw = board::setup_switch_on();
  g_switch_since = board::now_ms();
  set_mode(g_switch ? Mode::Setup : Mode::Station);
  return xTaskCreatePinnedToCore(relay_task, "mava-relay", 12288, nullptr, 1, nullptr, 0) == pdPASS;
}

void loop(Millis now) {
  bool raw = board::setup_switch_on();
  if (raw != g_switch_raw) {
    g_switch_raw = raw;
    g_switch_since = now;
  } else if (raw != g_switch && now - g_switch_since >= kDebounce) {
    g_switch = raw;
    set_mode(g_switch ? Mode::Setup : Mode::Station);
  }
  if (g_mode == Mode::Setup) {
    g_dns.processNextRequest();
    g_web.handleClient();
  }
  if (g_mode == Mode::Station && !g_time_given) {
    time_t t = time(nullptr);
    if (t > 1700000000) {
      g_node->set_time(uint32_t(t), now);
      g_time_given = true;
    }
  }
}

Mode mode() { return g_mode; }
bool wifi_up() { return g_wifi_up; }
const char* relay_state() { return const_cast<const char*>(g_relay_state); }

}  // namespace net
