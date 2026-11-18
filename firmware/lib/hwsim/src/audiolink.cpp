#include "audiolink.h"

#include "meshaudio.h"

namespace mesh {
namespace hwsim {

size_t encode_format(uint8_t* out, size_t cap) {
  if (cap < 5) return 0;
  out[0] = kAudioFormat;
  out[1] = uint8_t(audio::kRate >> 8);
  out[2] = uint8_t(audio::kRate);
  out[3] = uint8_t(audio::kFrame >> 8);
  out[4] = uint8_t(audio::kFrame);
  return 5;
}

size_t encode_speaker(uint16_t seq, const int16_t* pcm, size_t n, uint8_t* out, size_t cap) {
  if (cap < kAudioHeader + 2 * n) return 0;
  out[0] = kAudioSpeaker;
  out[1] = uint8_t(seq >> 8);
  out[2] = uint8_t(seq);
  for (size_t i = 0; i < n; i++) {
    uint16_t v = uint16_t(pcm[i]);
    out[kAudioHeader + 2 * i] = uint8_t(v);
    out[kAudioHeader + 2 * i + 1] = uint8_t(v >> 8);
  }
  return kAudioHeader + 2 * n;
}

size_t decode_mic(const uint8_t* a, size_t n, uint16_t* seq, int16_t* pcm, size_t cap) {
  if (n < kAudioHeader + 2 || a[0] != kAudioMic || (n - kAudioHeader) % 2 != 0) return 0;
  size_t samples = (n - kAudioHeader) / 2;
  if (samples > audio::kFrame || samples > cap) return 0;
  if (seq) *seq = uint16_t(a[1] << 8 | a[2]);
  for (size_t i = 0; i < samples; i++)
    pcm[i] = int16_t(uint16_t(a[kAudioHeader + 2 * i]) | uint16_t(a[kAudioHeader + 2 * i + 1]) << 8);
  return samples;
}

}  // namespace hwsim
}  // namespace mesh
