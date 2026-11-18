#include <cstdio>
#include <string>

#include "check.h"

int run_vectors(const char* path);
void run_units();
void run_route();
void run_mac();
void run_net();
void run_store();
void run_node();
void run_meshtastic();
void run_keys();
void run_kbproto();
void run_osk();
void run_mirror();
void run_client();
void run_audio();
void run_call(const std::string& dir);
void run_direct(const std::string& dir);

int main(int argc, char** argv) {
  const char* vectors = argc > 1 ? argv[1] : "../spec/test-vectors/vectors.json";
  if (run_vectors(vectors) != 0) return 1;
  run_units();
  run_route();
  run_mac();
  run_net();
  run_store();
  run_node();
  run_meshtastic();
  run_keys();
  run_kbproto();
  run_osk();
  run_mirror();
  run_client();
  run_audio();
  {
    std::string v = vectors;
    size_t slash = v.find_last_of('/');
    std::string dir = slash == std::string::npos ? std::string(".") : v.substr(0, slash);
    run_call(dir);
    run_direct(dir);
  }
  if (check::failures() != 0) {
    std::fprintf(stderr, "%d check(s) failed\n", check::failures());
    return 1;
  }
  std::printf("all firmware host tests passed\n");
  return 0;
}
