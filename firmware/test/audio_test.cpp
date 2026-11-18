// The board's sounds and the phone-as-speaker / microphone frames
// (lib/meshaudio, lib/hwsim audiolink).
#include <cstdlib>
#include <vector>

#include "audiolink.h"
#include "check.h"
#include "meshaudio.h"

namespace ma = mesh::audio;
namespace hw = mesh::hwsim;

namespace {

// Renders a whole sound; returns the frames that carried sound.
size_t frames_of(ma::Synth& s, int32_t* peak = nullptr) {
  size_t n = 0, sounding = 0;
  int16_t f[ma::kFrame];
  int32_t p = 0;
  while (s.busy() && n < 200) {
    if (s.render(f, ma::kFrame)) sounding++;
    for (int16_t v : f) p = std::abs(int32_t(v)) > p ? std::abs(int32_t(v)) : p;
    n++;
  }
  if (peak) *peak = p;
  return sounding;
}

void synth() {
  ma::Synth s;
  int16_t f[ma::kFrame];
  CHECK(!s.busy() && !s.render(f, ma::kFrame) && f[0] == 0 && f[ma::kFrame - 1] == 0);
  // Message chime: 90 + 30 + 140 ms = 26 frames of 10 ms, 23 of them with sound; below full scale.
  s.play(ma::Sound::Message);
  CHECK(s.busy() && s.playing() == ma::Sound::Message);
  int32_t peak = 0;
  size_t sounding = frames_of(s, &peak);
  CHECK(sounding >= 22 && sounding <= 26 && peak > 8000 && peak <= 9000 && !s.busy());
  // Fades in: the first samples are quiet (no click).
  s.play(ma::Sound::Test);
  CHECK(s.render(f, ma::kFrame));
  CHECK(std::abs(int32_t(f[0])) < 200 && std::abs(int32_t(f[1])) < 2000);
  // A key click does not cut the test tone short; a message replaces it.
  s.play(ma::Sound::Key);
  CHECK(s.playing() == ma::Sound::Test);
  s.play(ma::Sound::Message);
  CHECK(s.playing() == ma::Sound::Message);
  // A key click alone: 15 ms, two frames.
  ma::Synth k;
  k.play(ma::Sound::Key);
  CHECK(frames_of(k) == 2);
  // The ring: 1 s, sound in 0.8 s of it.
  ma::Synth r;
  r.play(ma::Sound::Ring);
  size_t ring = frames_of(r);
  CHECK(ring >= 78 && ring <= 82);
}

void meter_and_mix() {
  ma::Meter m;
  std::vector<int16_t> loud(ma::kFrame, 16384), quiet(ma::kFrame, 0);
  m.add(loud.data(), loud.size());
  CHECK(m.level() == 50 && m.frames() == 1);
  for (int i = 0; i < 40; i++) m.add(quiet.data(), quiet.size());
  CHECK(m.level() == 0 && m.frames() == 41);
  int16_t a[3] = {30000, -30000, 100};
  int16_t b[3] = {10000, -10000, -50};
  ma::mix(a, b, 3);
  CHECK(a[0] == 32767 && a[1] == -32768 && a[2] == 50);
}

void link() {
  uint8_t buf[hw::kAudioHeader + 2 * ma::kFrame];
  CHECK(hw::encode_format(buf, sizeof buf) == 5);
  CHECK((check::Bytes(buf, buf + 5) == check::Bytes{0, 0x3e, 0x80, 0x00, 0xa0}));  // 16000 Hz, 160 samples
  int16_t pcm[ma::kFrame];
  for (size_t i = 0; i < ma::kFrame; i++) pcm[i] = int16_t(int(i) * 300 - 24000);
  size_t n = hw::encode_speaker(0x1234, pcm, ma::kFrame, buf, sizeof buf);
  CHECK(n == 3 + 320 && buf[0] == hw::kAudioSpeaker && buf[1] == 0x12 && buf[2] == 0x34);
  CHECK(buf[3] == uint8_t(uint16_t(pcm[0])) && buf[4] == uint8_t(uint16_t(pcm[0]) >> 8));  // little-endian
  CHECK(hw::encode_speaker(1, pcm, ma::kFrame, buf, 100) == 0);
  // A mic frame has the speaker frame's layout with kind 2.
  buf[0] = hw::kAudioMic;
  int16_t back[ma::kFrame];
  uint16_t seq = 0;
  CHECK(hw::decode_mic(buf, n, &seq, back, ma::kFrame) == ma::kFrame && seq == 0x1234);
  bool same = true;
  for (size_t i = 0; i < ma::kFrame; i++) same = same && back[i] == pcm[i];
  CHECK(same);
  CHECK(hw::decode_mic(buf, n - 1, &seq, back, ma::kFrame) == 0);  // half a sample
  CHECK(hw::decode_mic(buf, 3, &seq, back, ma::kFrame) == 0);      // no samples
  buf[0] = hw::kAudioSpeaker;
  CHECK(hw::decode_mic(buf, n, &seq, back, ma::kFrame) == 0);  // not a mic frame
  std::vector<uint8_t> big(3 + 2 * (ma::kFrame + 1), 0);
  big[0] = hw::kAudioMic;
  CHECK(hw::decode_mic(big.data(), big.size(), &seq, back, ma::kFrame) == 0);  // over a frame
}

}  // namespace

void run_audio() {
  synth();
  meter_and_mix();
  link();
}
