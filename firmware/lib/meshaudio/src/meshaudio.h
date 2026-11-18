// The board's sounds and microphone, apart from any hardware (docs/screen.md
// "Audio"): a synth renders the UI's sounds (message chime, key click, test
// tone, ring) as 16 kHz mono signed 16-bit PCM in 10 ms frames (calls use
// 16 kHz; 10 ms keeps a frame under the host link's 512 bytes), a meter follows the
// microphone. Whatever carries the frames — the WM8960 codec over I2S, or
// the pre-release phone simulator (hwsim) — uses this format.
// Pure C++, no heap, no Arduino: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace audio {

constexpr int kRate = 16000;    // samples per second
constexpr size_t kFrame = 160;  // samples per frame
constexpr uint32_t kFrameMs = 10;

// Ring: an incoming call (played again every 3 s while it rings); Busy: the
// call ended (three short low notes).
enum class Sound : uint8_t { None = 0, Message = 1, Key = 2, Test = 3, Ring = 4, Busy = 5 };

class Synth {
 public:
  // Starts a sound. A key click does not cut a message chime or a test short.
  void play(Sound s);
  bool busy() const { return note_ < count_; }
  Sound playing() const { return busy() ? current_ : Sound::None; }
  // Writes the next n samples (silence when idle); false if it was all silence.
  bool render(int16_t* out, size_t n);

 private:
  struct Note {
    uint16_t hz;   // 0: a rest
    uint16_t ms;
    int16_t amp;   // peak, 0..32767
  };
  static constexpr size_t kMaxNotes = 4;
  Note notes_[kMaxNotes] = {};
  size_t count_ = 0, note_ = 0;
  uint32_t pos_ = 0;    // sample within the current note
  uint32_t phase_ = 0;  // phase accumulator (2^32 = one cycle)
  Sound current_ = Sound::None;
};

// Microphone level: the peak of the last frames, falling by 1/8 per frame,
// as 0..100 % of full scale.
class Meter {
 public:
  void add(const int16_t* pcm, size_t n);
  int level() const { return int(int32_t(peak_) * 100 / 32767); }
  uint32_t frames() const { return frames_; }

 private:
  int32_t peak_ = 0;
  uint32_t frames_ = 0;
};

// a += b, clipped to 16 bits.
void mix(int16_t* a, const int16_t* b, size_t n);

}  // namespace audio
}  // namespace mesh
