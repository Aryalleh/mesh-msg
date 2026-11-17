#include "remote.h"

#include <Arduino.h>
#include <esp_heap_caps.h>

#include "hostlink.h"
#include "mirror.h"
#include "ui.h"

namespace remote {
namespace {

namespace mm = mesh::mirror;
namespace host = mesh::host;

mm::Mirror* g_mirror = nullptr;
int g_port = -1;  // subscribed host port
bool g_send_info = false;
constexpr int kTilesPerLoop = 64;  // bounded by the port's output room

int get16(const uint8_t* p) { return int(p[0]) << 8 | p[1]; }

}  // namespace

bool begin() {
  if (g_mirror) return true;
  void* fb = heap_caps_malloc(sizeof(uint16_t) * mm::kWidth * mm::kHeight, MALLOC_CAP_SPIRAM);
  if (!fb) return false;
  static mm::Mirror m(static_cast<uint16_t*>(fb));
  g_mirror = &m;
  return true;
}

void blit(int x1, int y1, int x2, int y2, const uint16_t* px) {
  if (g_mirror) g_mirror->blit(x1, y1, x2, y2, px);
}

bool active() { return g_port >= 0; }

uint8_t command(mesh::node::Node& node, size_t port, uint8_t op, const uint8_t* a, size_t n) {
  (void)node;
  if (op == host::kOpScreen) {
    if (!g_mirror || !ui::ready()) return host::kAckUnknownOp;  // no screen UI on this build
    if (n != 1 || a[0] > 1) return host::kAckBadArgs;
    if (a[0] == 1) {
      g_port = int(port);
      g_send_info = true;
      g_mirror->mark_all();
    } else if (g_port == int(port)) {
      g_port = -1;
    }
    return host::kAckOk;
  }
  if (op == host::kOpInput) {
    if (!ui::ready()) return host::kAckUnknownOp;
    if (n != mm::kInputSize) return host::kAckBadArgs;
    if (a[0] == mm::kInputTouch) {
      int x = get16(a + 1), y = get16(a + 3);
      if (x >= mm::kWidth || y >= mm::kHeight || a[5] > 1) return host::kAckBadArgs;
      ui::remote_touch(x, y, a[5] == 1);
      return host::kAckOk;
    }
    if (a[0] == mm::kInputOption) {
      ui::remote_option(a[1], a[2]);
      return host::kAckOk;
    }
    if (a[0] == mm::kInputKey) {
      uint32_t cp = uint32_t(a[2]) << 24 | uint32_t(a[3]) << 16 | uint32_t(a[4]) << 8 | a[5];
      return ui::remote_key(a[1], cp) ? host::kAckOk : host::kAckBadArgs;
    }
    return host::kAckBadArgs;
  }
  return host::kAckUnknownOp;
}

void pump(mesh::node::Node& node) {
  if (g_port < 0 || !g_mirror) return;
  size_t port = size_t(g_port);
  if (!node.host_attached(port)) {
    g_port = -1;
    return;
  }
  uint8_t buf[mm::kMaxTile];
  if (g_send_info) {
    size_t n = mm::Mirror::info(buf, sizeof buf);
    if (!node.host_event(port, host::kEvScreen, buf, n)) return;
    g_send_info = false;
  }
  for (int i = 0; i < kTilesPerLoop && g_mirror->pending(); i++) {
    if (node.host_room(port) < mm::kMaxTile + 64) break;  // leave the rest for the next loop
    size_t n = g_mirror->next(buf, sizeof buf);
    if (!n || !node.host_event(port, host::kEvScreen, buf, n)) break;
  }
}

}  // namespace remote
