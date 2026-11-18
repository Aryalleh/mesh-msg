// The handheld's messaging client (lib/meshclient) as the UI's Messenger,
// plugged into the node on its own host port (Node::kLocalPort): the node
// hands it every packet it carries, and carries what it sends on LoRa,
// Bluetooth, USB and the relay. Main loop only (node lock held for pump).
#pragma once

#include <string>

#include "messenger.h"
#include "node.h"

namespace ui {

// Loads (or creates) the user identity and the conversations from flash.
// name: the nickname announced (e.g. "Mava-7514").
Messenger* client_begin(const char* name);
// Moves packets between the client and the node; announces when due.
void client_pump(mesh::node::Node& node, mesh::route::Millis now);
// For the log: on / waiting for the clock.
std::string client_state();
// The call task started (calls.h).
bool calls_on();

}  // namespace ui
