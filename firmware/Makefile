# Host build of the board-independent firmware libraries.
#   make test     build and run the tests (ASan + UBSan) against spec/test-vectors

CC ?= cc
CXX ?= c++
BUILD := build
SAN := -fsanitize=address,undefined -fno-omit-frame-pointer -fno-sanitize-recover=all
INC := -Ilib/meshproto/src -Ilib/meshroute/src -Ilib/monocypher/src
CFLAGS := -std=c99 -O1 -g $(SAN) $(INC)
CXXFLAGS := -std=c++17 -O1 -g -Wall -Wextra -Wpedantic -Wshadow -Werror $(SAN) $(INC)

LIB_C := lib/monocypher/src/monocypher.c lib/monocypher/src/monocypher-ed25519.c
LIB_CXX := $(wildcard lib/meshproto/src/*.cpp lib/meshroute/src/*.cpp)
TEST_CXX := $(wildcard test/*.cpp)
OBJS := $(LIB_C:%.c=$(BUILD)/%.o) $(LIB_CXX:%.cpp=$(BUILD)/%.o) $(TEST_CXX:%.cpp=$(BUILD)/%.o)
HDRS := $(wildcard lib/meshproto/src/*.h lib/meshroute/src/*.h test/*.h)

.PHONY: test clean
test: $(BUILD)/host_tests
	./$(BUILD)/host_tests ../spec/test-vectors/vectors.json

$(BUILD)/host_tests: $(OBJS)
	$(CXX) $(CXXFLAGS) -o $@ $^

$(BUILD)/%.o: %.c
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -c -o $@ $<

$(BUILD)/%.o: %.cpp $(HDRS)
	@mkdir -p $(dir $@)
	$(CXX) $(CXXFLAGS) -c -o $@ $<

clean:
	rm -rf $(BUILD)
