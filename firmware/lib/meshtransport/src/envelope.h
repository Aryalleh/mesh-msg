#pragma once
#include <cstddef>
#include <cstdint>
namespace mesh::transport {
constexpr uint8_t kMagic0=0x4D,kMagic1=0x56,kVersion=1;
constexpr size_t kHeaderSize=17;
constexpr size_t kMeshtasticPayloadMax=233;
constexpr size_t kChunkBodyMax=kMeshtasticPayloadMax-kHeaderSize;

enum class Kind:uint8_t{Data=1,Ack=2,Cancel=3};
struct Envelope{Kind kind=Kind::Data;uint8_t flags=0;uint8_t transfer_id[8]={};uint16_t index=0,total=0;const uint8_t* body=nullptr;size_t body_len=0;};
enum class Error:uint8_t{Ok=0,Short,Magic,Version,Kind,Range,TooLarge};
Error encode(const Envelope&,uint8_t*,size_t,size_t*);
Error decode(const uint8_t*,size_t,Envelope&);
bool looks_like_mava(const uint8_t*,size_t);
}
