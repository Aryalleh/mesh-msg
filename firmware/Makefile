# Host build of the board-independent firmware libraries.
#   make test     build and run the tests (ASan + UBSan) against spec/test-vectors
#   make sim      build build/meshnode-sim (real nodes on a simulated radio, ptys for meshd)

CC ?= cc
CXX ?= c++
BUILD := build
SAN := -fsanitize=address,undefined -fno-omit-frame-pointer -fno-sanitize-recover=all
INC := -Ilib/meshproto/src -Ilib/meshroute/src -Ilib/meshnode/src -Ilib/monocypher/src
CFLAGS := -std=c99 -O1 -g $(SAN) $(INC)
CXXFLAGS := -std=c++17 -O1 -g -Wall -Wextra -Wpedantic -Wshadow -Werror $(SAN) $(INC)

LIB_C := lib/monocypher/src/monocypher.c lib/monocypher/src/monocypher-ed25519.c
LIB_CXX := $(wildcard lib/meshproto/src/*.cpp lib/meshroute/src/*.cpp lib/meshnode/src/*.cpp)
TEST_CXX := $(wildcard test/*.cpp)
OBJS := $(LIB_C:%.c=$(BUILD)/%.o) $(LIB_CXX:%.cpp=$(BUILD)/%.o) $(TEST_CXX:%.cpp=$(BUILD)/%.o)
HDRS := $(wildcard lib/meshproto/src/*.h lib/meshroute/src/*.h lib/meshnode/src/*.h test/*.h)

.PHONY: test sim clean
test: $(BUILD)/host_tests
	./$(BUILD)/host_tests ../spec/test-vectors/vectors.json

$(BUILD)/host_tests: $(OBJS)
	$(CXX) $(CXXFLAGS) -o $@ $^

# Native node simulator (real nodes on a simulated radio, one pty per node).
LIB_OBJS := $(LIB_C:%.c=$(BUILD)/%.o) $(LIB_CXX:%.cpp=$(BUILD)/%.o)
sim: $(BUILD)/meshnode-sim
$(BUILD)/meshnode-sim: $(LIB_OBJS) $(BUILD)/sim/meshsim.o
	$(CXX) $(CXXFLAGS) -o $@ $^

$(BUILD)/%.o: %.c
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -c -o $@ $<

$(BUILD)/%.o: %.cpp $(HDRS)
	@mkdir -p $(dir $@)
	$(CXX) $(CXXFLAGS) -c -o $@ $<

clean:
	rm -rf $(BUILD)
