#include "hwsim.h"

#if MAVA_HWSIM

#include <Arduino.h>
#include <esp_heap_caps.h>

#include "../board_audio.h"
#include "../ui/ui.h"
#include "audiolink.h"
#include "mirror.h"

namespace hwsim {
namespace {

namespace mm = mesh::mirror;
namespace ma = mesh::audio;
namespace host = mesh::host;

mm::Mirror* g_mirror = nullptr;
int g_port = -1;  // the port subscribed to the screen
bool g_send_info = false;
constexpr int kTilesPerLoop = 64;  // bounded by the port's output room

int g_audio_port = -1;  // the port that is the speaker / microphone
bool g_speaker = false, g_mic = false, g_send_format = false;
uint16_t g_seq = 0;

int get16(const uint8_t* p) { return int(p[0]) << 8 | p[1]; }

void speaker(bool on) {
  if (on != g_speaker) audio::output_on(on);
  g_speaker = on;
}

void stop_audio() {
  speaker(false);
  g_mic = false;
  g_audio_port = -1;
}

uint8_t screen(size_t port, const uint8_t* a, size_t n) {
  if (!g_mirror || !ui::ready()) return host::kAckUnknownOp;  // no screen UI on this build
  if (n != 1 || a[0] > 1) return host::kAckBadArgs;
  if (a[0] == 1) {
    g_port = int(port);
    g_send_info = true;
    g_mirror->mark_all();
  } else if (g_port == int(port)) {
    g_port = -1;  // the phone keeps INPUT (keys and keyboard only)
  }
  return host::kAckOk;
}

uint8_t input(const uint8_t* a, size_t n) {
  if (!ui::ready()) return host::kAckUnknownOp;
  if (n != mm::kInputSize) return host::kAckBadArgs;
  if (a[0] == mm::kInputTouch) {
    int x = get16(a + 1), y = get16(a + 3);
    int w = g_mirror ? g_mirror->width() : mm::kWidth, h = g_mirror ? g_mirror->height() : mm::kHeight;
    if (x >= w || y >= h || a[5] > 1) return host::kAckBadArgs;
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

uint8_t audio_cmd(size_t port, const uint8_t* a, size_t n) {
  if (n < 1) return host::kAckBadArgs;
  switch (a[0]) {
    case mesh::hwsim::kAudioStop:
      if (n != 1) return host::kAckBadArgs;
      if (g_audio_port == int(port)) stop_audio();
      return host::kAckOk;
    case mesh::hwsim::kAudioStart: {
      if (n != 2 || a[1] > (mesh::hwsim::kFlagSpeaker | mesh::hwsim::kFlagMic)) return host::kAckBadArgs;
      g_audio_port = int(port);
      speaker((a[1] & mesh::hwsim::kFlagSpeaker) != 0);
      g_mic = (a[1] & mesh::hwsim::kFlagMic) != 0;
      g_send_format = true;
      if (!g_speaker && !g_mic) g_audio_port = -1;
      return host::kAckOk;
    }
    case mesh::hwsim::kAudioMic: {
      if (!g_mic || g_audio_port != int(port)) return host::kAckNotAllowed;
      int16_t pcm[ma::kFrame];
      size_t k = mesh::hwsim::decode_mic(a, n, nullptr, pcm, ma::kFrame);
      if (!k) return host::kAckBadArgs;
      audio::mic(pcm, k);
      return host::kAckOk;
    }
    default:
      return host::kAckBadArgs;
  }
}

}  // namespace

bool begin() {
  if (g_mirror) return true;
  void* fb = heap_caps_malloc(sizeof(uint16_t) * mm::kWidth * mm::kHeight, MALLOC_CAP_SPIRAM);
  if (!fb) return false;
  static mm::Mirror m(static_cast<uint16_t*>(fb));
  g_mirror = &m;
  return true;
}

void resize(int w, int h) {
  if (g_mirror && g_mirror->set_size(w, h)) g_send_info = true;  // the phone takes the new size
}

void blit(int x1, int y1, int x2, int y2, const uint16_t* px) {
  if (g_mirror) g_mirror->blit(x1, y1, x2, y2, px);
}

bool active() { return g_port >= 0 || g_audio_port >= 0; }

uint8_t command(mesh::node::Node& node, size_t port, uint8_t op, const uint8_t* a, size_t n) {
  (void)node;
  if (op == host::kOpScreen) return screen(port, a, n);
  if (op == host::kOpInput) return input(a, n);
  if (op == host::kOpAudio) return audio_cmd(port, a, n);
  return host::kAckUnknownOp;
}

void pump(mesh::node::Node& node) {
  if (g_audio_port >= 0 && !node.host_attached(size_t(g_audio_port))) stop_audio();
  if (g_audio_port >= 0) {
    size_t port = size_t(g_audio_port);
    uint8_t buf[mesh::hwsim::kAudioHeader + 2 * ma::kFrame];
    if (g_send_format) {
      size_t n = mesh::hwsim::encode_format(buf, sizeof buf);
      if (node.host_event(port, host::kEvAudio, buf, n)) g_send_format = false;
    }
    // Speaker frames first: late sound is worse than a late tile.
    int16_t f[ma::kFrame];
    while (g_speaker && node.host_room(port) >= sizeof buf + 64 && audio::take(f)) {
      size_t n = mesh::hwsim::encode_speaker(g_seq++, f, ma::kFrame, buf, sizeof buf);
      if (!node.host_event(port, host::kEvAudio, buf, n)) break;
    }
  }

  if (g_port < 0 || !g_mirror) return;
  size_t port = size_t(g_port);
  if (!node.host_attached(port)) {
    g_port = -1;
    return;
  }
  uint8_t buf[mm::kMaxTile];
  if (g_send_info) {
    size_t n = g_mirror->info(buf, sizeof buf);
    if (!node.host_event(port, host::kEvScreen, buf, n)) return;
    g_send_info = false;
  }
  for (int i = 0; i < kTilesPerLoop && g_mirror->pending(); i++) {
    if (node.host_room(port) < mm::kMaxTile + 64) break;  // leave the rest for the next loop
    size_t n = g_mirror->next(buf, sizeof buf);
    if (!n || !node.host_event(port, host::kEvScreen, buf, n)) break;
  }
}

}  // namespace hwsim

#endif  // MAVA_HWSIM
