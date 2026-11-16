#include "envelope.h"
#include <cstring>
namespace mesh::transport {
static bool valid_kind(uint8_t k){return k>=1&&k<=3;}
Error encode(const Envelope&e,uint8_t*out,size_t cap,size_t*out_len){
 if(e.total==0||e.index>=e.total)return Error::Range;
 if(e.body_len>kChunkBodyMax)return Error::TooLarge;
 if(e.body_len&&e.body==nullptr)return Error::Range;
 if(cap<kHeaderSize+e.body_len)return Error::Short;
 out[0]=kMagic0;out[1]=kMagic1;out[2]=kVersion;out[3]=static_cast<uint8_t>(e.kind);out[4]=e.flags;
 std::memcpy(out+5,e.transfer_id,8);out[13]=static_cast<uint8_t>(e.index>>8);out[14]=static_cast<uint8_t>(e.index);
 out[15]=static_cast<uint8_t>(e.total>>8);out[16]=static_cast<uint8_t>(e.total);
 if(e.body_len)std::memcpy(out+kHeaderSize,e.body,e.body_len);*out_len=kHeaderSize+e.body_len;return Error::Ok;
}
Error decode(const uint8_t*in,size_t len,Envelope&out){
 if(len<kHeaderSize)return Error::Short;if(in[0]!=kMagic0||in[1]!=kMagic1)return Error::Magic;if(in[2]!=kVersion)return Error::Version;if(!valid_kind(in[3]))return Error::Kind;
 Envelope e;e.kind=static_cast<Kind>(in[3]);e.flags=in[4];std::memcpy(e.transfer_id,in+5,8);e.index=static_cast<uint16_t>(in[13]<<8|in[14]);e.total=static_cast<uint16_t>(in[15]<<8|in[16]);
 if(e.total==0||e.index>=e.total)return Error::Range;e.body=in+kHeaderSize;e.body_len=len-kHeaderSize;if(e.body_len>kChunkBodyMax)return Error::TooLarge;out=e;return Error::Ok;
}
bool looks_like_mava(const uint8_t*in,size_t len){return len>=kHeaderSize&&in[0]==kMagic0&&in[1]==kMagic1&&in[2]==kVersion&&valid_kind(in[3]);}
}
