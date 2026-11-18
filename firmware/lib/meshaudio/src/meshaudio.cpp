#include "meshaudio.h"

#include <cmath>
#include <cstring>

namespace mesh {
namespace audio {
namespace {

constexpr uint32_t kRamp = kRate / 500;  // 2 ms fade in and out: no clicks

int16_t sine(uint32_t phase) {
  static int16_t table[256];
  static bool ready = false;
  if (!ready) {
    for (int i = 0; i < 256; i++) table[i] = int16_t(std::lround(32767.0 * std::sin(2.0 * 3.14159265358979 * i / 256.0)));
    ready = true;
  }
  return table[phase >> 24];
}

}  // namespace

void Synth::play(Sound s) {
  if (s == Sound::Key && busy() && current_ != Sound::Key) return;
  count_ = 0;
  switch (s) {
    case Sound::Message:  // two rising notes
      notes_[count_++] = {880, 90, 9000};
      notes_[count_++] = {0, 30, 0};
      notes_[count_++] = {1320, 140, 9000};
      break;
    case Sound::Key:
      notes_[count_++] = {1800, 15, 4000};
      break;
    case Sound::Test:
      notes_[count_++] = {440, 400, 12000};
      notes_[count_++] = {0, 100, 0};
      notes_[count_++] = {660, 400, 12000};
      break;
    case Sound::Ring:  // two short bursts of a two-tone trill
      notes_[count_++] = {880, 400, 10000};
      notes_[count_++] = {0, 200, 0};
      notes_[count_++] = {1100, 400, 10000};
      break;
    case Sound::Busy:
      notes_[count_++] = {480, 150, 9000};
      notes_[count_++] = {0, 100, 0};
      notes_[count_++] = {480, 150, 9000};
      notes_[count_++] = {0, 100, 0};
      break;
    case Sound::Ringback:
      notes_[count_++] = {425, 1000, 6000};
      break;
    case Sound::None:
      break;
  }
  note_ = 0;
  pos_ = 0;
  phase_ = 0;
  current_ = s;
}

bool Synth::render(int16_t* out, size_t n) {
  bool sound = false;
  for (size_t i = 0; i < n; i++) {
    int16_t v = 0;
    while (note_ < count_ && pos_ >= uint32_t(notes_[note_].ms) * kRate / 1000) {
      note_++;
      pos_ = 0;
      phase_ = 0;
    }
    if (note_ < count_) {
      const Note& nt = notes_[note_];
      const uint32_t len = uint32_t(nt.ms) * kRate / 1000;
      if (nt.hz) {
        int32_t amp = nt.amp;
        if (pos_ < kRamp) amp = amp * int32_t(pos_) / int32_t(kRamp);
        if (len - pos_ < kRamp) amp = amp * int32_t(len - pos_) / int32_t(kRamp);
        v = int16_t(int32_t(sine(phase_)) * amp / 32767);
        phase_ += uint32_t((uint64_t(nt.hz) << 32) / kRate);
        sound = true;
      }
      pos_++;
    }
    out[i] = v;
  }
  return sound;
}

void Meter::add(const int16_t* pcm, size_t n) {
  int32_t peak = 0;
  for (size_t i = 0; i < n; i++) {
    int32_t v = pcm[i] < 0 ? -int32_t(pcm[i]) : pcm[i];
    if (v > peak) peak = v;
  }
  if (peak > 32767) peak = 32767;
  peak_ = peak > peak_ ? peak : peak_ - peak_ / 8;
  frames_++;
}

void mix(int16_t* a, const int16_t* b, size_t n) {
  for (size_t i = 0; i < n; i++) {
    int32_t v = int32_t(a[i]) + b[i];
    a[i] = int16_t(v > 32767 ? 32767 : v < -32768 ? -32768 : v);
  }
}

}  // namespace audio
}  // namespace mesh
