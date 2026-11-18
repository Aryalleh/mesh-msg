// The board's audio (docs/screen.md "Audio"), kept in every build: sounds
// rendered on time into a short queue that the outputs take 20 ms frames
// from, and microphone frames from the inputs (meter, echo test). Outputs
// and inputs: the pre-release phone simulator (hwsim) now, the WM8960 codec
// later. Main loop only (no lock of its own).
#pragma once

#include <cstddef>
#include <cstdint>

#include "meshaudio.h"  // lib/meshaudio (mesh::audio)

namespace audio {

using mesh::audio::Sound;

void play(Sound s);
// Renders a frame every 20 ms while an output is on (catches up after a stall).
void loop(uint32_t now_ms);

// Outputs say when they take frames; take() hands out the next one.
void output_on(bool on);
int outputs();
bool take(int16_t out[mesh::audio::kFrame]);

// A microphone frame (any input).
void mic(const int16_t* pcm, size_t n);
int mic_level();
uint32_t mic_frames();
// Echo test: what the microphone hears goes back out (use headphones).
void set_echo(bool on);
bool echo();

// For the log: outputs, frames, microphone level.
void status(char* out, size_t cap);

}  // namespace audio
