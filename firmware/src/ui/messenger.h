// What the handheld UI needs from a messaging client. The handheld does not
// have a client core yet (docs/handheld-proto.md §8): the stub keeps notes on
// the device so the screens, keyboards and input can be built and tested now.
// A real client (C++ port of core/, decisions.md D21, or a phone over BLE)
// implements the same interface later.
#pragma once

#include <cstdint>
#include <string>
#include <vector>

namespace ui {

struct Contact {
  std::string id, name;
};

struct Message {
  bool mine = true;
  std::string text;
  uint32_t time = 0;  // Unix seconds, 0 = unknown
};

class Messenger {
 public:
  virtual ~Messenger() = default;
  // False: no client core; the UI says so and works on local notes only.
  virtual bool connected() const = 0;
  virtual std::vector<Contact> contacts() = 0;
  virtual std::vector<Message> messages(const std::string& contact) = 0;
  virtual bool send(const std::string& contact, const std::string& text, uint32_t now) = 0;
  virtual bool call(const std::string& contact) = 0;
  virtual void hang_up() = 0;
};

Messenger& stub_messenger();

}  // namespace ui
