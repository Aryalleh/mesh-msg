#include "presence_node.h"

#include <Arduino.h>
#include <WiFi.h>

#include <cstdio>
#include <cstring>

#include "ble.h"
#include "board.h"
#include "net.h"
#include "presence.h"
#include "wallclock.h"

namespace presence_node {
namespace {

namespace pr = mesh::presence;
constexpr const char* kFile = "/presence.bin";

Config g_cfg;
pr::Collector g_col;
uint32_t g_beacon_w = 0;

// One report waiting for the uploader task (the newest wins if it is still busy).
SemaphoreHandle_t g_mu = nullptr;
TaskHandle_t g_task = nullptr;
char g_body[pr::kMaxHeard * 40 + 64];
char g_sig[65];
size_t g_body_n = 0;
uint32_t g_sent = 0, g_failed = 0, g_dropped = 0;
int g_last_code = 0;

void uploader(void*) {
  static char body[sizeof g_body];
  char sig[65];
  for (;;) {
    ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
    size_t n;
    xSemaphoreTake(g_mu, portMAX_DELAY);
    n = g_body_n;
    memcpy(body, g_body, n + 1);
    memcpy(sig, g_sig, sizeof sig);
    g_body_n = 0;
    xSemaphoreGive(g_mu);
    if (!n || WiFi.status() != WL_CONNECTED) {
      g_dropped++;
      continue;
    }
    int code = net::post_presence(g_cfg.url, g_cfg.node_id, sig, body, n);
    g_last_code = code;
    if (code == 200) g_sent++;
    else g_failed++;
  }
}

void queue_report() {
  char sig[65];
  static char body[sizeof g_body];
  size_t n = g_col.report_body(body, sizeof body);
  if (!n) return;
  pr::report_sig(g_cfg.key, body, n, sig);
  xSemaphoreTake(g_mu, portMAX_DELAY);
  if (g_body_n) g_dropped++;  // the last one never went out
  memcpy(g_body, body, n + 1);
  memcpy(g_sig, sig, sizeof g_sig);
  g_body_n = n;
  xSemaphoreGive(g_mu);
  if (g_task) xTaskNotifyGive(g_task);
}

bool usable(const Config& c) { return c.on && c.node_id && strncmp(c.url, "http", 4) == 0; }

}  // namespace

void begin() {
  Config c;
  if (board::storage_read(kFile, &c, sizeof c) == sizeof c && c.magic == kMagic) g_cfg = c;
  g_mu = xSemaphoreCreateMutex();
  xTaskCreatePinnedToCore(uploader, "presence-up", 8192, nullptr, 1, &g_task, 0);
}

const Config& config() { return g_cfg; }

bool set(const Config& c) {
  g_cfg = c;
  g_cfg.magic = kMagic;
  g_beacon_w = 0;
  if (!usable(g_cfg)) ble::presence_beacon(nullptr);
  return board::storage_write(kFile, &g_cfg, sizeof g_cfg);
}

void loop() {
  if (!usable(g_cfg)) return;
  uint32_t t = wallclock::trusted_now();
  if (!t) return;
  uint32_t w = pr::window_of(t);
  if (w != g_beacon_w) {
    // new window: post what the last one heard (also an empty list: the admin sees the node alive), new beacon
    if (g_beacon_w && g_col.window() == g_beacon_w) queue_report();
    g_col.reset(w);
    uint8_t tok[pr::kTokenLen], adv[pr::kAdvLen];
    pr::beacon_token(g_cfg.key, w, tok);
    pr::build_adv('V', g_cfg.node_id, tok, adv);
    ble::presence_beacon(adv);
    g_beacon_w = w;
  }
  uint8_t tok[pr::kTokenLen];
  int8_t rssi;
  for (int i = 0; i < 32 && ble::presence_pop(tok, &rssi); i++) g_col.add(w, tok, rssi);
}

void status(char* out, size_t cap) {
  if (!g_cfg.on) {
    snprintf(out, cap, "presence off");
    return;
  }
  snprintf(out, cap, "presence node %lu%s, phones this window %u, reports sent %lu, failed %lu (last HTTP %d), dropped %lu",
           (unsigned long)g_cfg.node_id, wallclock::trusted() ? "" : " (waiting for the clock)", unsigned(g_col.size()),
           (unsigned long)g_sent, (unsigned long)g_failed, g_last_code, (unsigned long)g_dropped);
}

}  // namespace presence_node
