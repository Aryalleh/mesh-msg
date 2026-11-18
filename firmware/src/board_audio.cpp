#include "board_audio.h"

#include <cstdio>
#include <cstring>

namespace audio {
namespace {

namespace ma = mesh::audio;

// A few frames of queue: an output a little late loses nothing, one that
// stopped taking loses the oldest.
constexpr size_t kQueue = 8;

struct Ring {
  int16_t f[kQueue][ma::kFrame];
  size_t head = 0, count = 0;
  void push(const int16_t* pcm) {
    if (count == kQueue) {  // full: drop the oldest
      head = (head + 1) % kQueue;
      count--;
    }
    std::memcpy(f[(head + count) % kQueue], pcm, sizeof f[0]);
    count++;
  }
  bool pop(int16_t* out) {
    if (!count) return false;
    std::memcpy(out, f[head], sizeof f[0]);
    head = (head + 1) % kQueue;
    count--;
    return true;
  }
};

ma::Synth g_synth;
ma::Meter g_meter;
Ring g_out, g_echo;
int g_outputs = 0;
bool g_echo_on = false;
uint32_t g_next = 0;
bool g_started = false;
uint32_t g_frames_out = 0, g_sound_frames = 0;

}  // namespace

void play(Sound s) { g_synth.play(s); }

void loop(uint32_t now_ms) {
  if (g_outputs <= 0) {
    g_started = false;
    int16_t scratch[ma::kFrame];
    if (g_synth.busy()) g_synth.render(scratch, ma::kFrame);  // nobody listens: let it run out
    return;
  }
  if (!g_started || int32_t(now_ms - g_next) > int32_t(5 * ma::kFrameMs)) {  // start, or after a long stall
    g_next = now_ms;
    g_started = true;
  }
  while (int32_t(now_ms - g_next) >= 0) {
    int16_t f[ma::kFrame];
    if (g_synth.render(f, ma::kFrame)) g_sound_frames++;
    int16_t e[ma::kFrame];
    if (g_echo.pop(e)) ma::mix(f, e, ma::kFrame);
    g_out.push(f);
    g_next += ma::kFrameMs;
  }
}

void output_on(bool on) {
  g_outputs += on ? 1 : -1;
  if (g_outputs < 0) g_outputs = 0;
  if (g_outputs == 0) g_out = Ring();
}

int outputs() { return g_outputs; }

bool take(int16_t out[ma::kFrame]) {
  if (!g_out.pop(out)) return false;
  g_frames_out++;
  return true;
}

void mic(const int16_t* pcm, size_t n) {
  g_meter.add(pcm, n);
  if (!g_echo_on || n != ma::kFrame) return;
  g_echo.push(pcm);
}

int mic_level() { return g_meter.level(); }

uint32_t mic_frames() { return g_meter.frames(); }

void set_echo(bool on) {
  g_echo_on = on;
  if (!on) g_echo = Ring();
}

bool echo() { return g_echo_on; }

void status(char* out, size_t cap) {
  snprintf(out, cap, "outputs %d, frames out %lu (with sound %lu), mic frames %lu, level %d%%, echo %s", g_outputs,
           (unsigned long)g_frames_out, (unsigned long)g_sound_frames, (unsigned long)g_meter.frames(), g_meter.level(),
           g_echo_on ? "on" : "off");
}

}  // namespace audio
