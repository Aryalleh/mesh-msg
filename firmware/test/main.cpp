#include <cstdio>

#include "check.h"

int run_vectors(const char* path);
void run_units();
void run_route();
void run_mac();
void run_net();
void run_store();

int main(int argc, char** argv) {
  const char* vectors = argc > 1 ? argv[1] : "../spec/test-vectors/vectors.json";
  if (run_vectors(vectors) != 0) return 1;
  run_units();
  run_route();
  run_mac();
  run_net();
  run_store();
  if (check::failures() != 0) {
    std::fprintf(stderr, "%d check(s) failed\n", check::failures());
    return 1;
  }
  std::printf("all firmware host tests passed\n");
  return 0;
}
