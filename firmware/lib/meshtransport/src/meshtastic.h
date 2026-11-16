#pragma once
#include <cstddef>
#include <cstdint>
#include "envelope.h"

namespace mesh::transport {
constexpr uint16_t kMavaPortNum=256;
constexpr size_t kMeshtasticDataPayloadMax=233;
Error encode_app(const Envelope&,uint8_t*,size_t,size_t*);
Error decode_app(uint16_t,const uint8_t*,size_t,Envelope&);
bool is_mava_port(uint16_t);
}
