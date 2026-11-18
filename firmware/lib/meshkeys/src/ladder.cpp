#include "ladder.h"

namespace mesh {
namespace keys {

const Action kLadderActions[kLadderKeys] = {Action::Up,   Action::Down, Action::Left, Action::Right, Action::Ok,
                                            Action::Back, Action::Menu, Action::Call, Action::End};

namespace {
// Each key's share of the supply in thousandths: R / (R + 10 k).
constexpr uint32_t kLevels[kLadderKeys] = {0, 91, 180, 320, 405, 500, 600, 688, 796};
constexpr uint32_t kNone = 1000;
}  // namespace

int ladder_key(uint32_t mv, uint32_t vcc_mv) {
  if (!vcc_mv) return -1;
  uint32_t level = mv * 1000 / vcc_mv;
  for (size_t i = 0; i < kLadderKeys; i++) {
    uint32_t next = i + 1 < kLadderKeys ? kLevels[i + 1] : kNone;
    if (level < (kLevels[i] + next) / 2) return int(i);
  }
  return -1;
}

Action Ladder::feed(uint32_t mv, uint32_t now) {
  int k = ladder_key(mv);
  if (k != last_) {
    last_ = k;
    since_ = now;
    return Action::None;
  }
  if (k == stable_) {
    if (k < 0) return Action::None;
    Action a = kLadderActions[k];
    bool arrow = a == Action::Up || a == Action::Down || a == Action::Left || a == Action::Right;
    if (arrow && now - pressed_at_ >= kRepeatAfter && now - repeated_at_ >= kRepeatEvery) {
      repeated_at_ = now;
      return a;
    }
    return Action::None;
  }
  if (now - since_ < kSettleMs) return Action::None;
  // A new steady state: a press (from none, or straight from another key), or the release.
  stable_ = k;
  if (k < 0) return Action::None;
  pressed_at_ = repeated_at_ = now;
  return kLadderActions[k];
}

}  // namespace keys
}  // namespace mesh
