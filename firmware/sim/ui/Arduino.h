// Host stand-in for the few Arduino calls the UI uses (sim/ui).
#pragma once
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
inline uint32_t g_sim_ms = 0;  // advanced by the simulator
inline uint32_t millis() { return g_sim_ms; }
template <class T, class L, class H>
inline T constrain(T v, L lo, H hi) { return v < T(lo) ? T(lo) : v > T(hi) ? T(hi) : v; }
inline long map(long x, long a, long b, long c, long d) { return (x - a) * (d - c) / (b - a) + c; }
