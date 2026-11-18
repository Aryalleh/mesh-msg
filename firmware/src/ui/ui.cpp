#include "ui.h"

#include <Arduino.h>
#include <lvgl.h>

#include <cstring>
#include <deque>
#include <vector>

#include "display.h"
#include "kbmodule.h"
#include "ladder.h"
#include "osk.h"
#include "video.h"

LV_FONT_DECLARE(mava_font_16);  // Vazirmatn 16 px (fonts/mava_font_16.c, OFL)

namespace ui {
namespace {

namespace k = mesh::keys;

constexpr uint32_t kSleepMs = 60000;
constexpr int kTrackStep = 6;  // trackpad counts per list step

// Navigation keys on one ADC pin (lib/meshkeys ladder.h): Up, Down, Left,
// Right, OK, Back, Menu, Call, End without the keyboard module.
#ifdef NAV_KEYS_PIN
mesh::keys::Ladder g_nav;
bool g_nav_on = false;
uint32_t g_nav_boot_mv = 0;
uint32_t g_nav_at = 0;
#endif

StatusFn g_status_fn = nullptr;
RadioFn g_radio_fn = nullptr;
SoundFn g_sound_fn = nullptr;
EchoFn g_echo_fn = nullptr;
Messenger* g_msg = nullptr;
Status g_status;
bool g_ready = false;
// Touches from a phone or PC showing the screen (hwsim.h), in order.
struct RemoteTouch {
  int16_t x, y;
  bool pressed;
};
std::deque<RemoteTouch> g_remote;
RemoteTouch g_remote_last = {0, 0, false};
lv_indev_t* g_remote_indev = nullptr;
// The touch keyboard can be switched off (the phone's keyboard types through
// Mava Screen, or a hardware keyboard is enough).
bool g_osk_off = false;

lv_font_t g_font;  // Vazirmatn (Persian + Latin), falling back to Montserrat for symbols
lv_group_t* g_group = nullptr;
lv_indev_t* g_keypad = nullptr;
lv_indev_t* g_encoder = nullptr;
std::deque<uint32_t> g_keys;
int g_enc_diff = 0;
bool g_enc_pressed = false;
uint8_t g_brightness = 200;
bool g_asleep = false;

enum class Screen : uint8_t { Home, Chats, Chat, Network, Web, Settings, MyContact, Call, TouchCal };
Screen g_screen = Screen::Home;
std::string g_chat;  // contact id of the open chat
// The call screen: shown while a call is live, then a moment after it ends.
Screen g_call_back = Screen::Home;  // where to return
uint32_t g_call_rev = 0;
CallView::State g_call_state = CallView::State::None;
lv_obj_t* g_call_info = nullptr;
lv_obj_t* g_video_img = nullptr;  // the far side's camera, behind the call screen
uint32_t g_video_at = 0;          // when its last frame came
uint32_t g_ring_at = 0, g_call_close_at = 0, g_call_info_at = 0, g_ringback_at = 0;
// Touch calibration: the cross (on the top layer, in screen pixels).
lv_obj_t* g_cross[2] = {nullptr, nullptr};
int g_cal_shown = -2;

// Widgets of the current screen that are refreshed.
lv_obj_t* g_bar = nullptr;       // status bar label
lv_obj_t* g_body = nullptr;      // screen content
lv_obj_t* g_info = nullptr;      // network / settings text
lv_obj_t* g_messages = nullptr;  // chat history
lv_obj_t* g_input = nullptr;     // chat text area
lv_obj_t* g_osk = nullptr;       // touch keyboard
lv_obj_t* g_home_grid = nullptr; // home buttons (two columns)
mesh::osk::Page g_osk_page = mesh::osk::Page::Persian;

void show(Screen s);

// ---- design system (docs/design.md): dark violet, neon focus ----
namespace col {
inline lv_color_t bg() { return lv_color_hex(0x0B0A14); }
inline lv_color_t surface() { return lv_color_hex(0x15122A); }
inline lv_color_t surface2() { return lv_color_hex(0x1E1A3A); }
inline lv_color_t border() { return lv_color_hex(0x3B2A6B); }
inline lv_color_t fg() { return lv_color_hex(0xECE8FF); }
inline lv_color_t muted() { return lv_color_hex(0xA9A2CF); }
inline lv_color_t primary() { return lv_color_hex(0x7C3AED); }
inline lv_color_t primary_hi() { return lv_color_hex(0xA78BFA); }
inline lv_color_t accent() { return lv_color_hex(0xD946EF); }
inline lv_color_t mine() { return lv_color_hex(0x2E1A5E); }
}  // namespace col

lv_style_t g_st_btn, g_st_btn_focus, g_st_btn_pressed, g_st_card, g_st_input, g_st_input_focus, g_st_key, g_st_key_pressed;

void styles_init() {
  lv_style_init(&g_st_btn);
  lv_style_set_bg_color(&g_st_btn, col::surface2());
  lv_style_set_bg_opa(&g_st_btn, LV_OPA_COVER);
  lv_style_set_border_color(&g_st_btn, col::border());
  lv_style_set_border_width(&g_st_btn, 1);
  lv_style_set_radius(&g_st_btn, 2);
  lv_style_set_text_color(&g_st_btn, col::fg());
  lv_style_set_shadow_width(&g_st_btn, 0);
  lv_style_set_min_height(&g_st_btn, 40);
  // keyboard / trackpad focus: a magenta edge with a violet glow
  lv_style_init(&g_st_btn_focus);
  lv_style_set_border_color(&g_st_btn_focus, col::accent());
  lv_style_set_border_width(&g_st_btn_focus, 2);
  lv_style_set_shadow_color(&g_st_btn_focus, col::primary());
  lv_style_set_shadow_width(&g_st_btn_focus, 12);
  lv_style_set_shadow_opa(&g_st_btn_focus, LV_OPA_70);
  lv_style_set_outline_width(&g_st_btn_focus, 0);
  lv_style_init(&g_st_btn_pressed);
  lv_style_set_bg_color(&g_st_btn_pressed, col::primary());
  lv_style_init(&g_st_card);
  lv_style_set_bg_color(&g_st_card, col::surface());
  lv_style_set_bg_opa(&g_st_card, LV_OPA_COVER);
  lv_style_set_border_color(&g_st_card, col::border());
  lv_style_set_border_width(&g_st_card, 1);
  lv_style_set_radius(&g_st_card, 2);
  lv_style_set_text_color(&g_st_card, col::fg());
  lv_style_init(&g_st_input);
  lv_style_set_bg_color(&g_st_input, col::bg());
  lv_style_set_border_color(&g_st_input, col::border());
  lv_style_set_border_width(&g_st_input, 1);
  lv_style_set_radius(&g_st_input, 2);
  lv_style_set_text_color(&g_st_input, col::fg());
  lv_style_init(&g_st_input_focus);
  lv_style_set_border_color(&g_st_input_focus, col::primary_hi());
  lv_style_set_shadow_color(&g_st_input_focus, col::primary());
  lv_style_set_shadow_width(&g_st_input_focus, 10);
  lv_style_set_shadow_opa(&g_st_input_focus, LV_OPA_60);
  lv_style_init(&g_st_key);
  lv_style_set_bg_color(&g_st_key, col::surface2());
  lv_style_set_bg_opa(&g_st_key, LV_OPA_COVER);
  lv_style_set_border_color(&g_st_key, col::border());
  lv_style_set_border_width(&g_st_key, 1);
  lv_style_set_radius(&g_st_key, 2);
  lv_style_set_text_color(&g_st_key, col::fg());
  lv_style_set_shadow_width(&g_st_key, 0);
  lv_style_init(&g_st_key_pressed);
  lv_style_set_bg_color(&g_st_key_pressed, col::primary());
  lv_style_set_text_color(&g_st_key_pressed, lv_color_white());
}

// ---- helpers ----

lv_obj_t* label(lv_obj_t* parent, const std::string& text) {
  lv_obj_t* l = lv_label_create(parent);
  lv_label_set_text(l, text.c_str());
  lv_obj_set_style_base_dir(l, LV_BASE_DIR_AUTO, 0);
  lv_label_set_long_mode(l, LV_LABEL_LONG_WRAP);
  lv_obj_set_width(l, lv_pct(100));
  return l;
}

lv_obj_t* button(lv_obj_t* parent, const std::string& text, lv_event_cb_t cb, void* user = nullptr) {
  lv_obj_t* b = lv_button_create(parent);
  lv_obj_set_width(b, lv_pct(100));
  lv_obj_t* l = lv_label_create(b);
  lv_label_set_text(l, text.c_str());
  lv_obj_set_style_base_dir(l, LV_BASE_DIR_AUTO, 0);
  lv_obj_center(l);
  lv_obj_add_event_cb(b, cb, LV_EVENT_CLICKED, user);
  lv_obj_add_style(b, &g_st_btn, 0);
  lv_obj_add_style(b, &g_st_btn_focus, LV_STATE_FOCUSED);
  lv_obj_add_style(b, &g_st_btn_focus, LV_STATE_FOCUS_KEY);
  lv_obj_add_style(b, &g_st_btn_pressed, LV_STATE_PRESSED);
  lv_group_add_obj(g_group, b);
  return b;
}

void toast(const char* text) {
  lv_obj_t* m = lv_msgbox_create(nullptr);
  lv_msgbox_add_text(m, text);
  lv_obj_t* close = lv_msgbox_add_close_button(m);
  (void)close;
  lv_timer_t* t = lv_timer_create([](lv_timer_t* tm) { lv_msgbox_close(static_cast<lv_obj_t*>(lv_timer_get_user_data(tm))); },
                                  2500, m);
  lv_timer_set_repeat_count(t, 1);
}

// Short: with the touch-only buttons the bar has ~200 px for this.
std::string bar_text() {
  const Status& s = g_status;
  std::string t;
  if (s.battery_pct >= 0) t += std::string(LV_SYMBOL_BATTERY_FULL) + std::to_string(s.battery_pct) + "% ";
  if (s.radio > 0) t += "LoRa " + std::to_string(s.neighbours) + " ";
  if (s.setup_mode) {
    t += LV_SYMBOL_SETTINGS " setup ";
  } else if (s.wifi) {
    // The relay: online or not (the full text is on the Network screen).
    bool online = s.relay.rfind("online", 0) == 0;
    t += std::string(LV_SYMBOL_WIFI) + (online ? " " LV_SYMBOL_OK " " : " " LV_SYMBOL_REFRESH " ");
  }
  if (s.ble) t += LV_SYMBOL_BLUETOOTH " ";
  if (s.usb) t += LV_SYMBOL_USB " ";
  if (kbmodule::present()) t += kbmodule::layout().persian() ? "فا" : "EN";
  return t;
}

// ---- touch keyboard ----

const char* g_osk_map[mesh::osk::kMaxButtons + 8];
lv_buttonmatrix_ctrl_t g_osk_ctrl[mesh::osk::kMaxButtons];

const char* osk_label(const char* l) {
  if (!std::strcmp(l, "<-")) return LV_SYMBOL_BACKSPACE;
  if (!std::strcmp(l, "Enter")) return LV_SYMBOL_NEW_LINE;
  if (!std::strcmp(l, "Shift")) return LV_SYMBOL_UP;
  if (!std::strcmp(l, "v")) return LV_SYMBOL_DOWN;
  return l;
}

void osk_set_page(mesh::osk::Page p) {
  g_osk_page = p;
  const mesh::osk::Layout& l = mesh::osk::layout(p);
  size_t m = 0, b = 0;
  for (size_t r = 0; r < l.row_count; r++) {
    for (size_t i = 0; i < l.rows[r]; i++, b++) {
      g_osk_map[m++] = osk_label(l.buttons[b].label);
      g_osk_ctrl[b] = lv_buttonmatrix_ctrl_t(l.buttons[b].width);
    }
    if (r + 1 < l.row_count) g_osk_map[m++] = "\n";
  }
  g_osk_map[m] = "";
  lv_buttonmatrix_set_map(g_osk, g_osk_map);
  lv_buttonmatrix_set_ctrl_map(g_osk, g_osk_ctrl);
}

void send_input();

void osk_event(lv_event_t* e) {
  uint32_t id = lv_buttonmatrix_get_selected_button(g_osk);
  if (id == LV_BUTTONMATRIX_BUTTON_NONE || !g_input) return;
  mesh::osk::Press p = mesh::osk::press(g_osk_page, id);
  (void)e;
  switch (p.kind) {
    case mesh::osk::Kind::Text: lv_textarea_add_text(g_input, p.text); break;
    case mesh::osk::Kind::Backspace: lv_textarea_delete_char(g_input); break;
    case mesh::osk::Kind::Enter: send_input(); break;
    case mesh::osk::Kind::Hide: lv_obj_add_flag(g_osk, LV_OBJ_FLAG_HIDDEN); return;
    default: break;
  }
  if (p.next != g_osk_page) osk_set_page(p.next);
}

void osk_create(lv_obj_t* parent) {
  g_osk = lv_buttonmatrix_create(parent);
  lv_obj_set_size(g_osk, lv_pct(100), 112);  // landscape: 240 px tall, keep the chat visible
  lv_obj_set_style_pad_all(g_osk, 2, 0);
  lv_obj_set_style_pad_gap(g_osk, 2, 0);
  lv_obj_set_style_bg_color(g_osk, col::bg(), 0);
  lv_obj_set_style_border_color(g_osk, col::accent(), 0);
  lv_obj_set_style_border_width(g_osk, 1, 0);
  lv_obj_set_style_border_side(g_osk, LV_BORDER_SIDE_TOP, 0);
  lv_obj_add_style(g_osk, &g_st_key, LV_PART_ITEMS);
  lv_obj_add_style(g_osk, &g_st_key_pressed, LV_PART_ITEMS | LV_STATE_PRESSED);
  lv_obj_add_event_cb(g_osk, osk_event, LV_EVENT_VALUE_CHANGED, nullptr);
  osk_set_page(kbmodule::present() && !kbmodule::layout().persian() ? mesh::osk::Page::Latin
                                                                      : mesh::osk::Page::Persian);
  // With a hardware keyboard the touch keyboard waits until the text area is tapped.
  if (g_osk_off || kbmodule::present() || !display::touch()) lv_obj_add_flag(g_osk, LV_OBJ_FLAG_HIDDEN);
}

// ---- screens ----

void back();
void go_home();

// A small touch button in the top bar (not in the key group: keys keep
// moving through the screen's own items).
lv_obj_t* bar_button(lv_obj_t* bar, const char* sym, lv_event_cb_t cb) {
  lv_obj_t* b = lv_button_create(bar);
  lv_obj_set_size(b, 40, 26);
  lv_obj_add_style(b, &g_st_btn, 0);
  lv_obj_add_style(b, &g_st_btn_pressed, LV_STATE_PRESSED);
  lv_obj_set_style_pad_all(b, 0, 0);
  lv_obj_remove_flag(b, LV_OBJ_FLAG_SCROLL_ON_FOCUS);
  lv_obj_t* l = lv_label_create(b);
  lv_label_set_text(l, sym);
  lv_obj_center(l);
  lv_obj_add_event_cb(b, cb, LV_EVENT_CLICKED, nullptr);
  return b;
}

void clear() {
  for (lv_obj_t*& c : g_cross)
    if (c) lv_obj_delete(c), c = nullptr;
  lv_group_remove_all_objs(g_group);
  lv_obj_t* scr = lv_screen_active();
  lv_obj_clean(scr);
  g_info = g_messages = g_input = g_osk = g_call_info = g_video_img = nullptr;
  lv_obj_set_flex_flow(scr, LV_FLEX_FLOW_COLUMN);
  lv_obj_set_style_pad_all(scr, 0, 0);
  lv_obj_set_style_pad_gap(scr, 0, 0);
  lv_obj_set_style_bg_color(scr, col::bg(), 0);
  lv_obj_set_style_bg_grad_color(scr, lv_color_hex(0x1A1033), 0);
  lv_obj_set_style_bg_grad_dir(scr, LV_GRAD_DIR_VER, 0);
  lv_obj_t* bar = lv_obj_create(scr);
  lv_obj_remove_style_all(bar);
  lv_obj_set_size(bar, lv_pct(100), LV_SIZE_CONTENT);
  lv_obj_set_flex_flow(bar, LV_FLEX_FLOW_ROW);
  lv_obj_set_flex_align(bar, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_CENTER, LV_FLEX_ALIGN_CENTER);
  lv_obj_set_style_pad_all(bar, 2, 0);
  lv_obj_set_style_pad_gap(bar, 4, 0);
  lv_obj_set_style_bg_opa(bar, LV_OPA_COVER, 0);
  lv_obj_set_style_bg_color(bar, col::surface(), 0);
  lv_obj_set_style_border_color(bar, col::accent(), 0);
  lv_obj_set_style_border_width(bar, 1, 0);
  lv_obj_set_style_border_side(bar, LV_BORDER_SIDE_BOTTOM, 0);
  lv_obj_remove_flag(bar, LV_OBJ_FLAG_SCROLLABLE);
  // Without a keyboard module, Back and Home (and the way back to a call)
  // are touch buttons here, where the connection details were.
  if (!kbmodule::present()) {
    if (g_screen != Screen::Home) {
      bar_button(bar, LV_SYMBOL_LEFT, [](lv_event_t*) { back(); });
      bar_button(bar, LV_SYMBOL_HOME, [](lv_event_t*) { go_home(); });
    }
    CallView::State cs = g_msg ? g_msg->call_view().state : CallView::State::None;
    if (g_screen != Screen::Call && cs != CallView::State::None && cs != CallView::State::Ended) {
      lv_obj_t* c = bar_button(bar, LV_SYMBOL_CALL, [](lv_event_t*) { show(Screen::Call); });
      lv_obj_set_style_bg_color(c, lv_color_hex(0x15803D), 0);
    }
  }
  g_bar = lv_label_create(bar);
  lv_obj_set_flex_grow(g_bar, 1);
  lv_obj_set_style_text_color(g_bar, col::primary_hi(), 0);
  lv_obj_set_style_text_align(g_bar, LV_TEXT_ALIGN_RIGHT, 0);
  lv_label_set_long_mode(g_bar, LV_LABEL_LONG_CLIP);
  lv_label_set_text(g_bar, bar_text().c_str());
  g_body = lv_obj_create(scr);
  lv_obj_set_width(g_body, lv_pct(100));
  lv_obj_set_flex_grow(g_body, 1);
  lv_obj_set_flex_flow(g_body, LV_FLEX_FLOW_COLUMN);
  lv_obj_set_style_pad_all(g_body, 6, 0);
  lv_obj_set_style_radius(g_body, 0, 0);
  lv_obj_set_style_border_width(g_body, 0, 0);
  lv_obj_set_style_bg_opa(g_body, LV_OPA_TRANSP, 0);
  lv_obj_set_style_text_color(g_body, col::fg(), 0);
}

void title(const char* t) {
  lv_obj_t* l = label(g_body, t);
  lv_obj_set_style_text_color(l, col::primary_hi(), 0);
}

const char* radio_text(int radio) {
  return radio > 0 ? "وصل / ok" : radio < 0 ? "خاموش، ماژول وصل نیست / off (no module)" : "پیدا نشد / NOT FOUND";
}

std::string network_text() {
  const Status& s = g_status;
  char f[24];
  snprintf(f, sizeof f, "%.3f MHz", s.freq_hz / 1e6);
  return "نام: " + s.name + "\nشناسه: " + s.id + "\nMeshtastic: " + s.meshtastic_id + "\nLoRa: " + radio_text(s.radio) +
         "\n" + s.region + " " + f + " " + std::to_string(s.tx_dbm) + " dBm\nکانال: " + s.channel + "\nهمسایه‌ها: " +
         std::to_string(s.neighbours) + "   ذخیره: " + std::to_string(s.stored) + "\nزمان پخش: " +
         std::to_string(s.air_used_s) + " / " + std::to_string(s.air_budget_s) + " s\nرله: " + s.relay +
         "\nUSB: " + (s.usb ? "وصل" : "—") + "   BLE: " + (s.ble ? "وصل" : "—");
}

std::string settings_text() {
  const Status& s = g_status;
  return "رمز دستگاه (هات‌اسپات و پنل): " + s.password + "\nبلوتوث: بدون رمز جفت‌شدن" +
         "\nماژول LoRa: " + radio_text(s.radio) +
         "\nصدا: " + (s.audio_outputs > 0 ? std::to_string(s.audio_outputs) + " خروجی" : std::string("بدون خروجی")) +
         " · میکروفن " + std::to_string(s.mic_level) + "%" +
         "\n\nتنظیم وای‌فای و رله: کلید setup را روشن کنید و به هات‌اسپات وصل شوید.\nزبان: Alt + Shift\nنور: Vol+ / Vol-";
}

void refresh_messages() {
  if (!g_messages) return;
  lv_obj_clean(g_messages);
  for (const Message& m : g_msg->messages(g_chat)) {
    lv_obj_t* l = label(g_messages, m.mine && m.delivered ? m.text + "  " LV_SYMBOL_OK : m.text);
    lv_obj_set_style_bg_opa(l, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(l, m.mine ? col::mine() : col::surface2(), 0);
    lv_obj_set_style_border_color(l, m.mine ? col::primary_hi() : col::border(), 0);
    lv_obj_set_style_border_width(l, 1, 0);
    lv_obj_set_style_text_color(l, col::fg(), 0);
    lv_obj_set_style_pad_all(l, 4, 0);
    lv_obj_set_style_radius(l, 2, 0);
  }
  lv_obj_scroll_to_y(g_messages, LV_COORD_MAX, LV_ANIM_OFF);
}

void send_input() {
  if (!g_input) return;
  std::string text = lv_textarea_get_text(g_input);
  if (text.empty()) return;
  if (g_msg->send(g_chat, text, g_status.unix_time)) {
    lv_textarea_set_text(g_input, "");
    refresh_messages();
  } else {
    toast("ارسال نشد / not sent");
  }
}

std::string call_text(const CallView& c) {
  using S = CallView::State;
  std::string t;
  switch (c.state) {
    case S::Outgoing: t = c.peer_ringing ? "زنگ می‌خورد… / Ringing…" : "در حال تماس… / Calling…"; break;
    case S::Ringing: t = c.video_offered ? "تماس تصویری (با صدا پاسخ می‌دهد) / Video call (answered with voice)"
                                        : "تماس ورودی / Incoming call"; break;
    case S::Connecting: t = "در حال وصل شدن… / Connecting…"; break;
    case S::Active: {
      char d[16];
      snprintf(d, sizeof d, "%02lu:%02lu", (unsigned long)(c.seconds / 60), (unsigned long)(c.seconds % 60));
      t = d;
      if (!c.code.empty()) t += "   کد / code " + c.code;
      t += c.path == 1 ? "\nهمین شبکه / same network" : c.path >= 2 ? "\nمستقیم از اینترنت / direct over the internet" : "";
      if (c.video) t += "  " LV_SYMBOL_VIDEO;
      if (c.muted) t += "\nمیکروفن خاموش / muted";
      if (c.peer_muted) t += "\nطرف مقابل بی‌صدا کرده / they muted";
      break;
    }
    case S::Ended: t = (c.missed ? "تماس از دست رفته / Missed call" : "پایان تماس / Call ended") +
                       (c.reason.empty() ? std::string() : " · " + c.reason); break;
    case S::None: break;
  }
  return t;
}

// The calibration cross at screen pixel (x, y).
void place_cross(int x, int y) {
  for (int i = 0; i < 2; i++) {
    if (!g_cross[i]) {
      g_cross[i] = lv_obj_create(lv_layer_top());
      lv_obj_remove_style_all(g_cross[i]);
      lv_obj_set_style_bg_opa(g_cross[i], LV_OPA_COVER, 0);
      lv_obj_set_style_bg_color(g_cross[i], lv_color_hex(0xFFD400), 0);
      lv_obj_remove_flag(g_cross[i], LV_OBJ_FLAG_CLICKABLE);
    }
  }
  lv_obj_set_size(g_cross[0], 25, 3);
  lv_obj_set_pos(g_cross[0], x - 12, y - 1);
  lv_obj_set_size(g_cross[1], 3, 25);
  lv_obj_set_pos(g_cross[1], x - 1, y - 12);
}

void call_contact(const std::string& id) {
  if (g_msg->call(id)) {
    g_call_back = Screen::Chat;
    return;
  }
  std::string why = g_msg->call_error();
  toast(why.empty() ? "تماس ممکن نیست / calls are not possible here" : why.c_str());
}

void open_chat(lv_event_t* e) {
  g_chat = *static_cast<std::string*>(lv_event_get_user_data(e));
  show(Screen::Chat);
}

void show(Screen s) {
  if (g_screen == Screen::TouchCal && s != Screen::TouchCal) display::touch_cal_cancel();
  g_screen = s;
  clear();
  static std::vector<std::string> ids;  // user data of the chat buttons
  switch (s) {
    case Screen::Home:
      {
        lv_obj_t* w = label(g_body, "MAVA");
        lv_obj_set_style_text_color(w, col::primary_hi(), 0);
        lv_obj_set_style_text_letter_space(w, 6, 0);
        lv_obj_set_style_text_align(w, LV_TEXT_ALIGN_CENTER, 0);
        lv_obj_set_style_base_dir(w, LV_BASE_DIR_LTR, 0);
      }
      {
        // Landscape: the four buttons in two columns under the wordmark.
        lv_obj_t* grid = lv_obj_create(g_body);
        lv_obj_remove_style_all(grid);
        lv_obj_set_size(grid, lv_pct(100), LV_SIZE_CONTENT);
        lv_obj_set_flex_flow(grid, LV_FLEX_FLOW_ROW_WRAP);
        lv_obj_set_style_pad_gap(grid, 6, 0);
        g_home_grid = grid;
      }
      button(g_home_grid, LV_SYMBOL_LIST "  گفتگوها / Chats", [](lv_event_t*) { show(Screen::Chats); });
      button(g_home_grid, LV_SYMBOL_GPS "  شبکه / Network", [](lv_event_t*) { show(Screen::Network); });
      button(g_home_grid, LV_SYMBOL_WIFI "  مرورگر / Web", [](lv_event_t*) { show(Screen::Web); });
      button(g_home_grid, LV_SYMBOL_SETTINGS "  تنظیمات / Settings", [](lv_event_t*) { show(Screen::Settings); });
      for (uint32_t i = 0; i < lv_obj_get_child_count(g_home_grid); i++)
        lv_obj_set_width(lv_obj_get_child(g_home_grid, int32_t(i)), lv_pct(48));
      break;
    case Screen::Chats: {
      title("گفتگوها");
      if (!g_msg->connected())
        label(g_body, "پیام‌رسان روی این دستگاه هنوز فعال نیست؛ فعلاً یادداشت‌ها محلی‌اند.\n"
                      "Messaging client not on the handheld yet: notes stay local.");
      else
        button(g_body, LV_SYMBOL_IMAGE "  مخاطب من / My contact", [](lv_event_t*) { show(Screen::MyContact); });
      if (g_msg->connected() && g_msg->contacts().empty())
        label(g_body, "هنوز کسی نیست. با برنامهٔ Mava کد «مخاطب من» را اسکن کنید و پیام بدهید؛ "
                      "کسانی هم که نزدیک‌اند خودشان اینجا می‌آیند.");
      ids.clear();
      for (const Contact& c : g_msg->contacts()) ids.push_back(c.id);
      size_t i = 0;
      for (const Contact& c : g_msg->contacts()) button(g_body, c.name, open_chat, &ids[i++]);
      break;
    }
    case Screen::Chat: {
      lv_obj_set_style_pad_all(g_body, 2, 0);
      {
        // Who, and a call button (touch; 📞 on a keyboard does the same).
        lv_obj_t* row = lv_obj_create(g_body);
        lv_obj_remove_style_all(row);
        lv_obj_set_size(row, lv_pct(100), LV_SIZE_CONTENT);
        lv_obj_set_flex_flow(row, LV_FLEX_FLOW_ROW);
        lv_obj_set_flex_align(row, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_CENTER, LV_FLEX_ALIGN_CENTER);
        lv_obj_set_style_pad_gap(row, 6, 0);
        std::string who = g_chat;
        for (const Contact& c : g_msg->contacts())
          if (c.id == g_chat) who = c.name;
        lv_obj_t* name = label(row, who);
        lv_obj_set_width(name, LV_SIZE_CONTENT);
        lv_obj_set_flex_grow(name, 1);
        lv_obj_set_style_text_color(name, col::primary_hi(), 0);
        if (g_msg->connected()) {
          lv_obj_t* b = button(row, LV_SYMBOL_CALL "  تماس / Call", [](lv_event_t*) { call_contact(g_chat); });
          lv_obj_set_width(b, LV_SIZE_CONTENT);
          lv_obj_set_style_bg_color(b, lv_color_hex(0x15803D), 0);
        }
      }
      g_messages = lv_obj_create(g_body);
      lv_obj_set_width(g_messages, lv_pct(100));
      lv_obj_set_flex_grow(g_messages, 1);
      lv_obj_set_flex_flow(g_messages, LV_FLEX_FLOW_COLUMN);
      lv_obj_set_style_pad_all(g_messages, 3, 0);
      lv_obj_add_style(g_messages, &g_st_card, 0);
      g_input = lv_textarea_create(g_body);
      lv_textarea_set_one_line(g_input, true);
      lv_textarea_set_placeholder_text(g_input, "پیام…");
      lv_obj_set_width(g_input, lv_pct(100));
      lv_obj_set_style_base_dir(g_input, LV_BASE_DIR_AUTO, 0);
      lv_obj_add_style(g_input, &g_st_input, 0);
      lv_obj_add_style(g_input, &g_st_input_focus, LV_STATE_FOCUSED);
      lv_obj_add_event_cb(g_input, [](lv_event_t*) { send_input(); }, LV_EVENT_READY, nullptr);
      lv_obj_add_event_cb(g_input,
                          [](lv_event_t* e) {
                            lv_indev_t* in = lv_event_get_indev(e);
                            if (g_osk && !g_osk_off && in && lv_indev_get_type(in) == LV_INDEV_TYPE_POINTER)
                              lv_obj_remove_flag(g_osk, LV_OBJ_FLAG_HIDDEN);
                          },
                          LV_EVENT_CLICKED, nullptr);
      lv_group_add_obj(g_group, g_input);
      lv_group_focus_obj(g_input);
      lv_group_set_editing(g_group, true);
      osk_create(lv_screen_active());
      refresh_messages();
      break;
    }
    case Screen::Network:
      title("شبکه / Network");
      g_info = label(g_body, network_text());
      break;
    case Screen::Web: {
      title("مرورگر آیفون / Web access");
      lv_obj_set_flex_align(g_body, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_CENTER, LV_FLEX_ALIGN_CENTER);
      bool up = !g_status.web_url.empty();
      label(g_body, up ? "در همین شبکه این نشانی را در مرورگر باز کنید:" : "وای‌فای وصل نیست: کلید setup یا تنظیم وای‌فای.");
      if (up) {
        lv_obj_t* u = label(g_body, g_status.web_url);
        lv_obj_set_style_base_dir(u, LV_BASE_DIR_LTR, 0);
        lv_obj_set_style_text_align(u, LV_TEXT_ALIGN_CENTER, 0);
        lv_obj_t* qr = lv_qrcode_create(g_body);
        lv_qrcode_set_size(qr, 130);
        lv_qrcode_update(qr, g_status.web_url.data(), uint32_t(g_status.web_url.size()));
        lv_obj_set_style_border_color(qr, lv_color_white(), 0);
        lv_obj_set_style_border_width(qr, 4, 0);
      }
      break;
    }
    case Screen::MyContact: {
      title("مخاطب من / My contact");
      lv_obj_set_flex_align(g_body, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_CENTER, LV_FLEX_ALIGN_CENTER);
      std::string link = g_msg->my_link();
      label(g_body, g_msg->my_name());
      lv_obj_t* qr = lv_qrcode_create(g_body);
      lv_qrcode_set_size(qr, 150);  // landscape: 240 px tall
      lv_qrcode_update(qr, link.data(), uint32_t(link.size()));
      lv_obj_set_style_border_color(qr, lv_color_white(), 0);
      lv_obj_set_style_border_width(qr, 4, 0);
      label(g_body, "در برنامهٔ Mava: افزودن مخاطب ← اسکن کد");
      break;
    }
    case Screen::Call: {
      CallView c = g_msg->call_view();
      g_call_state = c.state;
      lv_obj_set_flex_align(g_body, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_CENTER, LV_FLEX_ALIGN_CENTER);
      if (c.video && c.state == CallView::State::Active) {
        // The other side's camera fills the screen behind the text and buttons
        // once frames come (loop() decodes them).
        lv_obj_remove_flag(g_body, LV_OBJ_FLAG_SCROLLABLE);
        g_video_img = lv_image_create(g_body);
        lv_obj_add_flag(g_video_img, LV_OBJ_FLAG_FLOATING);
        lv_obj_add_flag(g_video_img, LV_OBJ_FLAG_HIDDEN);
        lv_obj_remove_flag(g_video_img, LV_OBJ_FLAG_CLICKABLE);
        lv_obj_align(g_video_img, LV_ALIGN_CENTER, 0, 0);
        g_video_at = 0;
      }
      lv_obj_t* who = label(g_body, LV_SYMBOL_CALL "  " + c.peer);
      lv_obj_set_style_text_color(who, col::primary_hi(), 0);
      lv_obj_set_style_text_align(who, LV_TEXT_ALIGN_CENTER, 0);
      g_call_info = label(g_body, call_text(c));
      lv_obj_set_style_text_align(g_call_info, LV_TEXT_ALIGN_CENTER, 0);
      if (g_video_img) {  // over a picture: text on a dark band
        for (lv_obj_t* l : {who, g_call_info}) {
          lv_obj_set_style_bg_opa(l, LV_OPA_60, 0);
          lv_obj_set_style_bg_color(l, lv_color_black(), 0);
        }
      }
      using S = CallView::State;
      if (c.state == S::Ringing) {
        lv_obj_t* a = button(g_body, LV_SYMBOL_CALL "  پاسخ / Answer", [](lv_event_t*) {
          if (!g_msg->answer()) toast("پاسخ ممکن نشد / could not answer");
        });
        lv_obj_set_style_bg_color(a, lv_color_hex(0x15803D), 0);
        lv_group_focus_obj(a);
        button(g_body, LV_SYMBOL_CLOSE "  رد / Decline", [](lv_event_t*) { g_msg->hang_up(); });
      } else if (c.state == S::Active) {
        button(g_body, c.muted ? LV_SYMBOL_AUDIO "  روشن کردن میکروفن / Unmute" : LV_SYMBOL_MUTE "  بی‌صدا / Mute",
               [](lv_event_t*) { g_msg->set_muted(!g_msg->call_view().muted); });
        lv_obj_t* h = button(g_body, LV_SYMBOL_CLOSE "  قطع / Hang up", [](lv_event_t*) { g_msg->hang_up(); });
        lv_obj_set_style_bg_color(h, lv_color_hex(0xB91C1C), 0);
      } else if (c.state != S::Ended && c.state != S::None) {
        lv_obj_t* h = button(g_body, LV_SYMBOL_CLOSE "  قطع / Hang up", [](lv_event_t*) { g_msg->hang_up(); });
        lv_obj_set_style_bg_color(h, lv_color_hex(0xB91C1C), 0);
        lv_group_focus_obj(h);
      }
      if (c.state == S::Active || c.state == S::Connecting)
        label(g_body, g_status.audio_outputs > 0 ? "" : "بلندگو نیست: در Mava Screen صدا و میکروفن را روشن کنید / "
                                                         "no speaker: turn on sound and microphone in Mava Screen");
      break;
    }
    case Screen::TouchCal: {
      title("تنظیم لمس / Touch calibration");
      label(g_body, "مرکز هر + زرد را با نوک قلم یا ناخن بزنید و بردارید (سه بار).\n"
                    "Tap the centre of each yellow + and lift (three times). Back cancels.");
      display::touch_cal_start();
      g_cal_shown = -2;  // loop() places the first cross
      break;
    }
    case Screen::Settings:
      title("تنظیمات / Settings");
      g_info = label(g_body, settings_text());
      if (g_radio_fn) {
        // Without a LoRa module the board skips the radio: no probe at boot
        // (it holds Bluetooth up to 20 s), nothing queued for the air.
        bool off = g_status.radio < 0;
        button(g_body, off ? "ماژول LoRa وصل است: روشن کن / LoRa module fitted" : "ماژول LoRa وصل نیست: خاموش کن / No LoRa module",
               [](lv_event_t*) {
                 g_radio_fn(g_status.radio < 0);
                 if (g_status_fn) g_status = g_status_fn();
                 toast(g_status.radio > 0 ? "LoRa: ok" : g_status.radio < 0 ? "LoRa: خاموش / off" : "LoRa: پیدا نشد / NOT FOUND");
                 show(Screen::Settings);
               });
      }
      if (display::touch())
        button(g_body, display::touch_calibrated() ? "تنظیم دوبارهٔ لمس / Calibrate touch again" : "تنظیم لمس / Calibrate touch",
               [](lv_event_t*) { show(Screen::TouchCal); });
      if (g_sound_fn)
        button(g_body, "آزمایش صدا / Sound test", [](lv_event_t*) { g_sound_fn(kSoundTest); });
      if (g_echo_fn)
        button(g_body, g_status.echo ? "پژواک میکروفن: خاموش کن / Echo test off" : "پژواک میکروفن (با هدفون) / Echo test",
               [](lv_event_t*) {
                 g_echo_fn(!g_status.echo);
                 if (g_status_fn) g_status = g_status_fn();
                 show(Screen::Settings);
               });
      break;
  }
}

void go_home() { show(Screen::Home); }

void back() {
  switch (g_screen) {
    case Screen::TouchCal: show(Screen::Settings); break;
    case Screen::Call:  // a live call stays on screen; End hangs up
      if (g_call_state == CallView::State::Ended || g_call_state == CallView::State::None) show(g_call_back);
      break;
    case Screen::Chat: show(Screen::Chats); break;
    case Screen::MyContact: show(Screen::Chats); break;
    case Screen::Home: break;
    default: show(Screen::Home); break;
  }
}

// ---- input ----

void keypad_cb(lv_indev_t*, lv_indev_data_t* d) {
  static bool released = true;
  if (!released) {  // every key is a press followed by a release
    d->state = LV_INDEV_STATE_RELEASED;
    released = true;
    return;
  }
  if (g_keys.empty()) {
    d->state = LV_INDEV_STATE_RELEASED;
    return;
  }
  d->key = g_keys.front();
  g_keys.pop_front();
  d->state = LV_INDEV_STATE_PRESSED;
  released = false;
  d->continue_reading = !g_keys.empty();
}

void remote_cb(lv_indev_t*, lv_indev_data_t* d) {
  if (!g_remote.empty()) {
    g_remote_last = g_remote.front();
    g_remote.pop_front();
  }
  d->point.x = g_remote_last.x;
  d->point.y = g_remote_last.y;
  d->state = g_remote_last.pressed ? LV_INDEV_STATE_PRESSED : LV_INDEV_STATE_RELEASED;
  d->continue_reading = !g_remote.empty();
}

void encoder_cb(lv_indev_t*, lv_indev_data_t* d) {
  d->enc_diff = int16_t(g_enc_diff);
  g_enc_diff = 0;
  d->state = g_enc_pressed ? LV_INDEV_STATE_PRESSED : LV_INDEV_STATE_RELEASED;
  g_enc_pressed = false;
}

void wake() {
  if (g_asleep) {
    g_asleep = false;
    display::backlight(g_brightness);
  }
  lv_display_trigger_activity(nullptr);
}

// LVGL's keypad hands a key to the text area as UTF-8 bytes packed into a
// uint32_t in memory order (lv_textarea_add_char), not as a code point.
uint32_t lv_key_of(char32_t cp) {
  uint8_t b[4] = {};
  if (cp < 0x80) {
    b[0] = uint8_t(cp);
  } else if (cp < 0x800) {
    b[0] = uint8_t(0xC0 | cp >> 6);
    b[1] = uint8_t(0x80 | (cp & 0x3F));
  } else if (cp < 0x10000) {
    b[0] = uint8_t(0xE0 | cp >> 12);
    b[1] = uint8_t(0x80 | (cp >> 6 & 0x3F));
    b[2] = uint8_t(0x80 | (cp & 0x3F));
  } else {
    b[0] = uint8_t(0xF0 | cp >> 18);
    b[1] = uint8_t(0x80 | (cp >> 12 & 0x3F));
    b[2] = uint8_t(0x80 | (cp >> 6 & 0x3F));
    b[3] = uint8_t(0x80 | (cp & 0x3F));
  }
  uint32_t v;
  std::memcpy(&v, b, 4);
  return v;
}

void on_output(const k::Output& o) {
  wake();
  if (g_sound_fn && o.action != k::Action::LanguageChanged) g_sound_fn(kSoundKey);
  switch (o.action) {
    case k::Action::Text: g_keys.push_back(lv_key_of(o.cp)); break;
    case k::Action::Backspace: g_keys.push_back(LV_KEY_BACKSPACE); break;
    case k::Action::Enter:
    case k::Action::Ok: g_keys.push_back(LV_KEY_ENTER); break;
    case k::Action::Up: g_keys.push_back(LV_KEY_PREV); break;
    case k::Action::Down: g_keys.push_back(LV_KEY_NEXT); break;
    case k::Action::Left: g_keys.push_back(LV_KEY_LEFT); break;
    case k::Action::Right: g_keys.push_back(LV_KEY_RIGHT); break;
    case k::Action::Back: back(); break;
    case k::Action::Menu: show(Screen::Home); break;
    case k::Action::End: {
      CallView::State st = g_msg->call_view().state;
      if (st != CallView::State::None && st != CallView::State::Ended) {
        g_msg->hang_up();  // also declines a ringing call
      } else {
        show(Screen::Home);
      }
      break;
    }
    case k::Action::Call: {
      CallView::State st = g_msg->call_view().state;
      if (st == CallView::State::Ringing) {
        if (!g_msg->answer()) toast("پاسخ ممکن نشد / could not answer");
      } else if (st != CallView::State::None && st != CallView::State::Ended) {
        if (g_screen != Screen::Call) show(Screen::Call);  // back to the call
      } else if (g_screen == Screen::Chat) {
        call_contact(g_chat);
      } else {
        toast("برای تماس یک گفتگو را باز کنید / open a chat to call");
      }
      break;
    }
    case k::Action::VolUp:
    case k::Action::VolDown:
      g_brightness = uint8_t(constrain(int(g_brightness) + (o.action == k::Action::VolUp ? 25 : -25), 20, 255));
      display::backlight(g_brightness);
      break;
    case k::Action::Symbols:
      if (g_osk) {
        osk_set_page(kbmodule::layout().persian() ? mesh::osk::Page::SymbolsPersian : mesh::osk::Page::SymbolsLatin);
        lv_obj_remove_flag(g_osk, LV_OBJ_FLAG_HIDDEN);
      }
      break;
    case k::Action::LanguageChanged:
      if (g_bar) lv_label_set_text(g_bar, bar_text().c_str());
      break;
    default: break;
  }
}

}  // namespace

void on_radio(RadioFn fn) { g_radio_fn = fn; }
void on_sound(SoundFn fn) { g_sound_fn = fn; }
void on_echo(EchoFn fn) { g_echo_fn = fn; }

bool begin(StatusFn status, Messenger& messenger) {
  g_status_fn = status;
  g_msg = &messenger;
  lv_init();
  lv_tick_set_cb([]() -> uint32_t { return millis(); });
  if (!display::begin()) return false;
  g_font = mava_font_16;
  g_font.fallback = &lv_font_montserrat_14;
  lv_display_t* disp = lv_display_get_default();
  lv_theme_t* th = lv_theme_default_init(disp, col::primary(), col::accent(), true, &g_font);
  styles_init();
  lv_display_set_theme(disp, th);
  g_group = lv_group_create();
  lv_group_set_default(g_group);
  g_keypad = lv_indev_create();
  lv_indev_set_type(g_keypad, LV_INDEV_TYPE_KEYPAD);
  lv_indev_set_read_cb(g_keypad, keypad_cb);
  lv_indev_set_group(g_keypad, g_group);
  if (kbmodule::variant() == k::Variant::BB9900Trackpad && kbmodule::present()) {
    g_encoder = lv_indev_create();
    lv_indev_set_type(g_encoder, LV_INDEV_TYPE_ENCODER);
    lv_indev_set_read_cb(g_encoder, encoder_cb);
    lv_indev_set_group(g_encoder, g_group);
  }
  g_remote_indev = lv_indev_create();
  lv_indev_set_type(g_remote_indev, LV_INDEV_TYPE_POINTER);
  lv_indev_set_read_cb(g_remote_indev, remote_cb);
#ifdef NAV_KEYS_PIN
  // Fitted when its 10 k pull-up holds the pin high: a pin left open floats
  // and would press keys at random.
  // Steady near the top (Arduino's default attenuation reads 0–3.1 V): an
  // open pin wanders, and a floating pin must never press keys.
  {
    uint32_t lo = 4000, hi = 0;
    for (int i = 0; i < 20; i++) {
      uint32_t mv = analogReadMilliVolts(NAV_KEYS_PIN);
      lo = mv < lo ? mv : lo;
      hi = mv > hi ? mv : hi;
      delay(5);
    }
    g_nav_on = lo >= 2900 && hi - lo < 150;
    g_nav_boot_mv = lo;
  }
#endif
  if (g_status_fn) g_status = g_status_fn();
  show(Screen::Home);
  display::backlight(g_brightness);
  g_ready = true;
  return true;
}

bool ready() { return g_ready; }

bool nav_keys() {
#ifdef NAV_KEYS_PIN
  return g_nav_on;
#else
  return false;
#endif
}

uint32_t nav_keys_mv() {
#ifdef NAV_KEYS_PIN
  return g_nav_boot_mv;
#else
  return 0;
#endif
}

void remote_touch(int x, int y, bool pressed) {
  if (!g_ready) return;
  wake();
  // The phone sends screen (landscape) coordinates; LVGL rotates pointer
  // input as if it came from the panel, so undo that first.
  lv_display_t* d = lv_display_get_default();
  int pw = lv_display_get_physical_horizontal_resolution(d), ph = lv_display_get_physical_vertical_resolution(d);
  switch (lv_display_get_rotation(d)) {
    case LV_DISPLAY_ROTATION_90: {
      int px = y, py = ph - 1 - x;
      x = px, y = py;
      break;
    }
    case LV_DISPLAY_ROTATION_270: {
      int px = pw - 1 - y, py = x;
      x = px, y = py;
      break;
    }
    case LV_DISPLAY_ROTATION_180:
      x = pw - 1 - x, y = ph - 1 - y;
      break;
    default:
      break;
  }
  if (g_remote.size() >= 32) g_remote.pop_front();  // a stalled UI keeps the latest
  g_remote.push_back({int16_t(x), int16_t(y), pressed});
}

void remote_option(uint8_t id, uint8_t value) {
  if (id != 1) return;  // mirror::kOptTouchKeyboard
  g_osk_off = value == 0;
  if (g_osk) {
    if (g_osk_off) lv_obj_add_flag(g_osk, LV_OBJ_FLAG_HIDDEN);
    else if (!kbmodule::present() && g_screen == Screen::Chat) lv_obj_remove_flag(g_osk, LV_OBJ_FLAG_HIDDEN);
  }
}

bool remote_key(uint8_t action, uint32_t cp) {
  if (!g_ready || action == uint8_t(k::Action::None) || action > uint8_t(k::Action::LanguageChanged)) return false;
  if (action == uint8_t(k::Action::Text) && (cp < 0x20 || cp > 0x10FFFF)) return false;
  on_output(k::Output{k::Action(action), char32_t(cp)});
  return true;
}

void loop(uint32_t now_ms) {
  if (!g_ready) return;
  // A call: its screen comes up (and wakes the display), follows its state,
  // rings while it rings, and goes away a moment after the end.
  uint32_t crev = g_msg->call_revision();
  if (crev != g_call_rev) {
    g_call_rev = crev;
    CallView c = g_msg->call_view();
    using S = CallView::State;
    bool live = c.state != S::None && c.state != S::Ended;
    if (live && g_screen != Screen::Call) {
      if (g_screen != Screen::Call) g_call_back = g_screen == Screen::Chat ? Screen::Chat : g_screen;
      wake();
      show(Screen::Call);
    } else if (g_screen == Screen::Call && c.state != g_call_state) {
      show(Screen::Call);
    }
    if (c.state == S::Ringing) g_ring_at = now_ms;  // ring now
    if (c.state == S::Ended && g_screen == Screen::Call) {
      g_call_close_at = now_ms + 2500;
      if (g_sound_fn) g_sound_fn(kSoundBusy);
    }
    if (c.state == S::Ended && c.missed && g_screen != Screen::Call) toast(("تماس از دست رفته / Missed call: " + c.peer).c_str());
    // The top bar's way back to the call comes and goes with it.
    if (!live && g_screen != Screen::Call && g_screen != Screen::Chat && g_screen != Screen::TouchCal &&
        !kbmodule::present())
      show(g_screen);
  }
  // Calling out: the ringback tone once their phone rings (every 4 s, as phones).
  if (g_call_state == CallView::State::Outgoing && g_screen == Screen::Call && now_ms - g_ringback_at >= 4000 &&
      g_msg->call_view().peer_ringing) {
    g_ringback_at = now_ms;
    if (g_sound_fn) g_sound_fn(kSoundRingback);
  }
  if (g_call_state == CallView::State::Ringing && g_ring_at && int32_t(now_ms - g_ring_at) >= 0) {
    if (g_sound_fn) g_sound_fn(kSoundRing);
    g_ring_at = now_ms + 3000;
  }
  {
    const uint8_t* jpeg = nullptr;
    size_t jn = 0;
    if (g_msg->take_video(&jpeg, &jn) && g_screen == Screen::Call && g_video_img) {
      const lv_image_dsc_t* img = video::decode(jpeg, jn, 320, 240 - 28);
      if (img) {
        lv_image_cache_drop(img);
        lv_image_set_src(g_video_img, nullptr);
        lv_image_set_src(g_video_img, img);
        lv_obj_remove_flag(g_video_img, LV_OBJ_FLAG_HIDDEN);
        lv_obj_move_background(g_video_img);
        lv_obj_invalidate(g_video_img);
        g_video_at = now_ms;
      }
    }
    if (g_video_img && g_video_at && now_ms - g_video_at > 2000) {  // the camera went off
      lv_obj_add_flag(g_video_img, LV_OBJ_FLAG_HIDDEN);
      g_video_at = 0;
    }
  }
  if (g_screen == Screen::Call && g_call_info && now_ms - g_call_info_at >= 500) {
    g_call_info_at = now_ms;
    lv_label_set_text(g_call_info, call_text(g_msg->call_view()).c_str());
  }
  if (g_call_close_at && int32_t(now_ms - g_call_close_at) >= 0) {
    g_call_close_at = 0;
    if (g_screen == Screen::Call && g_call_state == CallView::State::Ended) show(g_call_back);
  }

  if (g_screen == Screen::TouchCal) {
    int step = display::touch_cal_step();
    if (step != g_cal_shown) {
      g_cal_shown = step;
      if (step >= 0 && step <= 2) {
        int x, y;
        display::touch_cal_target(step, &x, &y);
        place_cross(x, y);
        if (step > 0 && g_sound_fn) g_sound_fn(kSoundKey);
      } else if (step == 3) {
        show(Screen::Settings);
        toast("لمس تنظیم شد / Touch calibrated");
      } else {
        toast("دوباره امتحان کنید / Try again: tap each + exactly");
        display::touch_cal_start();
        g_cal_shown = -2;
      }
    }
  }

  // New messages, receipts or people: redraw what shows them.
  static uint32_t seen_rev = 0;
  uint32_t rev = g_msg->revision();
  if (rev != seen_rev) {
    seen_rev = rev;
    if (g_screen == Screen::Chat) refresh_messages();
    else if (g_screen == Screen::Chats) show(Screen::Chats);
  }
  static uint32_t last_status = 0;
  static int track_y = 0;
  k::Output out[8];
  int dx = 0, dy = 0;
  size_t n = kbmodule::poll(now_ms, out, 8, &dx, &dy);
  for (size_t i = 0; i < n; i++) on_output(out[i]);
#ifdef NAV_KEYS_PIN
  if (g_nav_on && now_ms - g_nav_at >= 10) {
    g_nav_at = now_ms;
    k::Action a = g_nav.feed(analogReadMilliVolts(NAV_KEYS_PIN), now_ms);
    if (a != k::Action::None) on_output(k::Output{a, 0});
  }
#endif
  if (dy) {
    wake();
    track_y += dy;
    while (track_y >= kTrackStep) {
      g_enc_diff++;
      track_y -= kTrackStep;
    }
    while (track_y <= -kTrackStep) {
      g_enc_diff--;
      track_y += kTrackStep;
    }
  }
  if (now_ms - last_status >= 1000 && g_status_fn) {
    last_status = now_ms;
    g_status = g_status_fn();
    if (g_bar) lv_label_set_text(g_bar, bar_text().c_str());
    if (g_info && g_screen == Screen::Network) lv_label_set_text(g_info, network_text().c_str());
  }
  if (!g_asleep && lv_display_get_inactive_time(nullptr) > kSleepMs) {
    g_asleep = true;
    display::backlight(0);
  } else if (g_asleep && lv_display_get_inactive_time(nullptr) < 200) {
    wake();  // a touch woke it
  }
  lv_timer_handler();
}

}  // namespace ui
