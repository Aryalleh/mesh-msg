// The phone as the board's speaker and microphone over the host link
// (docs/screen.md "Audio"), a pre-release hardware simulator (MAVA_HWSIM):
// 16 kHz mono signed 16-bit PCM in 10 ms frames (mesh::audio).
//
// AUDIO command arguments (host::kOpAudio):
//   stop:  0
//   start: 1 | flags u8        kFlagSpeaker: send speaker frames, kFlagMic: microphone frames follow
//   mic:   2 | seq u16 BE | pcm  s16 little-endian, at most audio::kFrame samples
// AUDIO event data (host::kEvAudio):
//   format:  0 | rate u16 BE | frame u16 BE   (after start)
//   speaker: 1 | seq u16 BE | pcm             s16 little-endian, audio::kFrame samples
// Pure C++, no heap, no Arduino: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace hwsim {

constexpr uint8_t kAudioStop = 0;
constexpr uint8_t kAudioStart = 1;
constexpr uint8_t kAudioMic = 2;
constexpr uint8_t kAudioFormat = 0;
constexpr uint8_t kAudioSpeaker = 1;
constexpr uint8_t kFlagSpeaker = 1;
constexpr uint8_t kFlagMic = 2;
constexpr size_t kAudioHeader = 3;

// Event data; 0 if cap is too small.
size_t encode_format(uint8_t* out, size_t cap);
size_t encode_speaker(uint16_t seq, const int16_t* pcm, size_t n, uint8_t* out, size_t cap);
// A microphone command's PCM into pcm (at most cap samples); the sample
// count, 0 if it is not a valid mic frame.
size_t decode_mic(const uint8_t* a, size_t n, uint16_t* seq, int16_t* pcm, size_t cap);

}  // namespace hwsim
}  // namespace mesh
