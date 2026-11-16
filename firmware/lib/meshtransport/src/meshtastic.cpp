#include "meshtastic.h"
namespace mesh::transport {
Error encode_app(const Envelope&e,uint8_t*out,size_t cap,size_t*out_len){return encode(e,out,cap,out_len);}
Error decode_app(uint16_t portnum,const uint8_t*payload,size_t len,Envelope&out){if(!is_mava_port(portnum))return Error::Kind;return decode(payload,len,out);}
bool is_mava_port(uint16_t portnum){return portnum==kMavaPortNum;}
}
