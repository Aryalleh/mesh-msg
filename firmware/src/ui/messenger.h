// What the handheld UI needs from a messaging client: the real one is
// lib/meshclient behind src/ui/client_messenger.cpp (docs/handheld-proto.md
// §8); the stub keeps local notes (the UI simulator, boards without storage).
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
  bool delivered = false;  // mine: a signed receipt came back
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
  // Our contact link (QR code for the apps to add us) and nickname; "" = none.
  virtual std::string my_link() { return ""; }
  virtual std::string my_name() { return ""; }
  // Grows when contacts or messages change: the UI refreshes.
  virtual uint32_t revision() { return 0; }
};

Messenger& stub_messenger();

}  // namespace ui
