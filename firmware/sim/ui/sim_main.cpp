// Renders the handheld UI on the host and writes screenshots (PPM) of each
// screen: make -C firmware uishots.
#include <cstdio>
#include <deque>
#include <string>

#include "../../src/ui/ui.h"
#include "Arduino.h"
#include "keys.h"

namespace sim {
extern uint16_t fb[320 * 240];
extern std::deque<mesh::keys::Output> keys;
extern bool module, touched;
extern int tx, ty;
}  // namespace sim

namespace {

using mesh::keys::Action;
using mesh::keys::Output;

ui::Status status() {
  ui::Status s;
  s.name = "Mava";
  s.id = "3f9a1c0be2d47a51";
  s.meshtastic_id = "!8c41d2a7";
  s.region = "EU_868";
  s.channel = "LongFast";
  s.relay = "online";
  s.web_url = "http://192.168.1.42/";
  s.password = "k7m3qpx2va";
  s.passkey = 482913;
  s.freq_hz = 869525000;
  s.tx_dbm = 14;
  s.neighbours = 3;
  s.stored = 12;
  s.air_used_s = 41;
  s.air_budget_s = 360;
  s.ble = true;
  s.wifi = true;
  s.battery_pct = 78;
  return s;
}

void run(uint32_t ms) {
  for (uint32_t t = 0; t < ms; t += 5) {
    g_sim_ms += 5;
    ui::loop(g_sim_ms);
  }
}

void key(Action a, char32_t cp = 0) {
  sim::keys.push_back(Output{a, cp});
  run(60);
}

void type(const std::u32string& s) {
  for (char32_t c : s) key(Action::Text, c);
}

void tap(int x, int y) {
  sim::tx = x;
  sim::ty = y;
  sim::touched = true;
  run(80);
  sim::touched = false;
  run(120);
}

void shot(const char* name) {
  run(100);
  std::string path = std::string(name) + ".ppm";
  FILE* f = fopen(path.c_str(), "wb");
  fprintf(f, "P6\n320 240\n255\n");
  for (uint16_t p : sim::fb) {
    unsigned char rgb[3] = {uint8_t((p >> 11 & 31) * 255 / 31), uint8_t((p >> 5 & 63) * 255 / 63),
                            uint8_t((p & 31) * 255 / 31)};
    fwrite(rgb, 1, 3, f);
  }
  fclose(f);
  printf("%s\n", path.c_str());
}

}  // namespace

int main() {
  ui::begin(status, ui::stub_messenger());
  run(1500);
  shot("1-home");
  key(Action::Ok);  // Chats
  shot("2-chats");
  key(Action::Ok);  // Notes
  type(U"سلام، این یک پیام آزمایشی است");
  key(Action::Enter);
  type(U"Mava ۱۴۰۵");
  key(Action::Enter);
  type(U"نیم‌فاصله");
  shot("3-chat-hardware-keyboard");
  key(Action::Symbols);  // Sym: the touch keyboard on the symbols page
  shot("4-chat-touch-symbols");
  key(Action::Menu);
  sim::module = false;  // no hardware keyboard: the touch keyboard opens by itself
  key(Action::Ok);
  key(Action::Ok);
  sim::keys.clear();
  shot("5-chat-touch-persian");
  tap(30, 236);  // a letter on the touch keyboard's first row
  tap(60, 236);
  shot("6-chat-touch-typed");
  sim::module = true;
  key(Action::Menu);
  key(Action::Down);
  key(Action::Ok);  // Network
  shot("7-network");
  key(Action::Back);
  key(Action::Down);
  key(Action::Down);
  key(Action::Ok);  // Web access
  shot("8-web-access");
  key(Action::Back);
  key(Action::Down);
  key(Action::Down);
  key(Action::Down);
  key(Action::Ok);  // Settings
  shot("9-settings");
  return 0;
}
