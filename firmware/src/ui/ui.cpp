#include "ui.h"

#include <Arduino.h>
#include <lvgl.h>

#include <cstring>
#include <deque>
#include <vector>

#include "display.h"
#include "kbmodule.h"
#include "osk.h"

LV_FONT_DECLARE(mava_font_16);  // Vazirmatn 16 px (fonts/mava_font_16.c, OFL)

namespace ui {
namespace {

namespace k = mesh::keys;

constexpr uint32_t kSleepMs = 60000;
constexpr int kTrackStep = 6;  // trackpad counts per list step

StatusFn g_status_fn = nullptr;
Messenger* g_msg = nullptr;
Status g_status;
bool g_ready = false;

lv_font_t g_font;  // Vazirmatn (Persian + Latin), falling back to Montserrat for symbols
lv_group_t* g_group = nullptr;
lv_indev_t* g_keypad = nullptr;
lv_indev_t* g_encoder = nullptr;
std::deque<uint32_t> g_keys;
int g_enc_diff = 0;
bool g_enc_pressed = false;
uint8_t g_brightness = 200;
bool g_asleep = false;

enum class Screen : uint8_t { Home, Chats, Chat, Network, Web, Settings };
Screen g_screen = Screen::Home;
std::string g_chat;  // contact id of the open chat

// Widgets of the current screen that are refreshed.
lv_obj_t* g_bar = nullptr;       // status bar label
lv_obj_t* g_body = nullptr;      // screen content
lv_obj_t* g_info = nullptr;      // network / settings text
lv_obj_t* g_messages = nullptr;  // chat history
lv_obj_t* g_input = nullptr;     // chat text area
lv_obj_t* g_osk = nullptr;       // touch keyboard
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

std::string bar_text() {
  const Status& s = g_status;
  std::string t;
  if (s.battery_pct >= 0) t += std::string(LV_SYMBOL_BATTERY_FULL " ") + std::to_string(s.battery_pct) + "%  ";
  t += "LoRa " + std::to_string(s.neighbours) + "  ";
  if (s.setup_mode) t += LV_SYMBOL_SETTINGS " setup  ";
  else if (s.wifi) t += std::string(LV_SYMBOL_WIFI " ") + s.relay + "  ";
  if (s.ble) t += LV_SYMBOL_BLUETOOTH " ";
  if (s.usb) t += LV_SYMBOL_USB " ";
  if (kbmodule::present()) t += kbmodule::layout().persian() ? " فا" : " EN";
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
  lv_obj_set_size(g_osk, lv_pct(100), 130);
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
  if (kbmodule::present() || !display::touch()) lv_obj_add_flag(g_osk, LV_OBJ_FLAG_HIDDEN);
}

// ---- screens ----

void clear() {
  lv_group_remove_all_objs(g_group);
  lv_obj_t* scr = lv_screen_active();
  lv_obj_clean(scr);
  g_info = g_messages = g_input = g_osk = nullptr;
  lv_obj_set_flex_flow(scr, LV_FLEX_FLOW_COLUMN);
  lv_obj_set_style_pad_all(scr, 0, 0);
  lv_obj_set_style_pad_gap(scr, 0, 0);
  lv_obj_set_style_bg_color(scr, col::bg(), 0);
  lv_obj_set_style_bg_grad_color(scr, lv_color_hex(0x1A1033), 0);
  lv_obj_set_style_bg_grad_dir(scr, LV_GRAD_DIR_VER, 0);
  g_bar = lv_label_create(scr);
  lv_obj_set_width(g_bar, lv_pct(100));
  lv_obj_set_style_pad_all(g_bar, 3, 0);
  lv_obj_set_style_bg_opa(g_bar, LV_OPA_COVER, 0);
  lv_obj_set_style_bg_color(g_bar, col::surface(), 0);
  lv_obj_set_style_text_color(g_bar, col::primary_hi(), 0);
  lv_obj_set_style_border_color(g_bar, col::accent(), 0);
  lv_obj_set_style_border_width(g_bar, 1, 0);
  lv_obj_set_style_border_side(g_bar, LV_BORDER_SIDE_BOTTOM, 0);
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

std::string network_text() {
  const Status& s = g_status;
  char f[24];
  snprintf(f, sizeof f, "%.3f MHz", s.freq_hz / 1e6);
  return "نام: " + s.name + "\nشناسه: " + s.id + "\nMeshtastic: " + s.meshtastic_id + "\nLoRa: " + s.region + " " +
         f + " " + std::to_string(s.tx_dbm) + " dBm\nکانال: " + s.channel + "\nهمسایه‌ها: " +
         std::to_string(s.neighbours) + "   ذخیره: " + std::to_string(s.stored) + "\nزمان پخش: " +
         std::to_string(s.air_used_s) + " / " + std::to_string(s.air_budget_s) + " s\nرله: " + s.relay +
         "\nUSB: " + (s.usb ? "وصل" : "—") + "   BLE: " + (s.ble ? "وصل" : "—");
}

std::string settings_text() {
  const Status& s = g_status;
  return "رمز دستگاه (هات‌اسپات و پنل): " + s.password + "\nرمز جفت‌شدن بلوتوث: " + std::to_string(s.passkey) +
         "\n\nتنظیم وای‌فای و رله: کلید setup را روشن کنید و به هات‌اسپات وصل شوید.\nزبان: Alt + Shift\nنور: Vol+ / Vol-";
}

void refresh_messages() {
  if (!g_messages) return;
  lv_obj_clean(g_messages);
  for (const Message& m : g_msg->messages(g_chat)) {
    lv_obj_t* l = label(g_messages, m.text);
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

void open_chat(lv_event_t* e) {
  g_chat = *static_cast<std::string*>(lv_event_get_user_data(e));
  show(Screen::Chat);
}

void show(Screen s) {
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
      button(g_body, LV_SYMBOL_LIST "  گفتگوها / Chats", [](lv_event_t*) { show(Screen::Chats); });
      button(g_body, LV_SYMBOL_GPS "  شبکه / Network", [](lv_event_t*) { show(Screen::Network); });
      button(g_body, LV_SYMBOL_WIFI "  مرورگر آیفون / Web access", [](lv_event_t*) { show(Screen::Web); });
      button(g_body, LV_SYMBOL_SETTINGS "  تنظیمات / Settings", [](lv_event_t*) { show(Screen::Settings); });
      break;
    case Screen::Chats: {
      title("گفتگوها");
      if (!g_msg->connected())
        label(g_body, "پیام‌رسان روی این دستگاه هنوز فعال نیست؛ فعلاً یادداشت‌ها محلی‌اند.\n"
                      "Messaging client not on the handheld yet: notes stay local.");
      ids.clear();
      for (const Contact& c : g_msg->contacts()) ids.push_back(c.id);
      size_t i = 0;
      for (const Contact& c : g_msg->contacts()) button(g_body, c.name, open_chat, &ids[i++]);
      break;
    }
    case Screen::Chat: {
      lv_obj_set_style_pad_all(g_body, 2, 0);
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
                            if (g_osk && in && lv_indev_get_type(in) == LV_INDEV_TYPE_POINTER)
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
    case Screen::Settings:
      title("تنظیمات / Settings");
      g_info = label(g_body, settings_text());
      break;
  }
}

void back() {
  switch (g_screen) {
    case Screen::Chat: show(Screen::Chats); break;
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
    case k::Action::End:
      g_msg->hang_up();
      show(Screen::Home);
      break;
    case k::Action::Call:
      if (g_screen == Screen::Chat && !g_msg->call(g_chat)) toast("تماس هنوز ممکن نیست / calls need the client");
      break;
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
  if (g_status_fn) g_status = g_status_fn();
  show(Screen::Home);
  display::backlight(g_brightness);
  g_ready = true;
  return true;
}

void loop(uint32_t now_ms) {
  if (!g_ready) return;
  static uint32_t last_status = 0;
  static int track_y = 0;
  k::Output out[8];
  int dx = 0, dy = 0;
  size_t n = kbmodule::poll(now_ms, out, 8, &dx, &dy);
  for (size_t i = 0; i < n; i++) on_output(out[i]);
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
