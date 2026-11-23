# Host build of the board-independent firmware libraries (needs OpenSSL's
# libcrypto for the Meshtastic channel AES).
#   make test     build and run the tests (ASan + UBSan) against spec/test-vectors
#   make sim      build build/meshnode-sim (real nodes on a simulated radio, ptys for meshd)
#   make callpeer build build/callpeer (the board's call manager on loopback UDP;
#                 core/call TestBoardPeer calls it from the Go phone code)

CC ?= cc
CXX ?= c++
BUILD := build
SAN := -fsanitize=address,undefined -fno-omit-frame-pointer -fno-sanitize-recover=all
INC := -Ilib/meshproto/src -Ilib/meshroute/src -Ilib/meshnode/src -Ilib/meshtastic/src -Ilib/meshkeys/src -Ilib/meshui/src -Ilib/meshclient/src -Ilib/hwsim/src -Ilib/meshaudio/src -Ilib/meshcall/src -Ilib/meshpresence/src -Ilib/monocypher/src
CFLAGS := -std=c99 -O1 -g $(SAN) $(INC)
CXXFLAGS := -std=c++17 -O1 -g -Wall -Wextra -Wpedantic -Wshadow -Werror $(SAN) $(INC)

LIB_C := lib/monocypher/src/monocypher.c lib/monocypher/src/monocypher-ed25519.c
LIB_CXX := $(wildcard lib/meshproto/src/*.cpp lib/meshroute/src/*.cpp lib/meshnode/src/*.cpp lib/meshtastic/src/*.cpp lib/meshkeys/src/*.cpp lib/meshui/src/*.cpp lib/meshclient/src/*.cpp lib/hwsim/src/*.cpp lib/meshaudio/src/*.cpp lib/meshcall/src/*.cpp lib/meshpresence/src/*.cpp)
TEST_CXX := $(wildcard test/*.cpp)
OBJS := $(LIB_C:%.c=$(BUILD)/%.o) $(LIB_CXX:%.cpp=$(BUILD)/%.o) $(TEST_CXX:%.cpp=$(BUILD)/%.o)
HDRS := $(wildcard lib/meshproto/src/*.h lib/meshroute/src/*.h lib/meshnode/src/*.h lib/meshtastic/src/*.h lib/meshkeys/src/*.h lib/meshui/src/*.h lib/meshclient/src/*.h lib/hwsim/src/*.h lib/meshaudio/src/*.h lib/meshcall/src/*.h lib/meshpresence/src/*.h test/*.h)

# The libraries run on the board in tasks with 6–16 kB of stack: no function
# may take more than 4 kB of it (Jitter::reset's 8.6 kB temporary overflowed
# the call task in beta36). The tests themselves may.
$(LIB_CXX:%.cpp=$(BUILD)/%.o): CXXFLAGS += -Wstack-usage=4096

.PHONY: test sim callpeer clean
test: $(BUILD)/host_tests
	./$(BUILD)/host_tests ../spec/test-vectors/vectors.json

$(BUILD)/host_tests: $(OBJS)
	$(CXX) $(CXXFLAGS) -o $@ $^ -lcrypto

# Native node simulator (real nodes on a simulated radio, one pty per node).
LIB_OBJS := $(LIB_C:%.c=$(BUILD)/%.o) $(LIB_CXX:%.cpp=$(BUILD)/%.o)
sim: $(BUILD)/meshnode-sim
$(BUILD)/meshnode-sim: $(LIB_OBJS) $(BUILD)/sim/meshsim.o
	$(CXX) $(CXXFLAGS) -o $@ $^ -lcrypto

callpeer: $(BUILD)/callpeer
$(BUILD)/callpeer: $(LIB_OBJS) $(BUILD)/sim/callpeer.o
	$(CXX) $(CXXFLAGS) -o $@ $^ -lcrypto

$(BUILD)/%.o: %.c
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -c -o $@ $<

$(BUILD)/%.o: %.cpp $(HDRS)
	@mkdir -p $(dir $@)
	$(CXX) $(CXXFLAGS) -c -o $@ $<

clean:
	rm -rf $(BUILD)

# Handheld UI on the host: renders every screen of src/ui into PPM screenshots
# (build/uishots/). Needs LVGL from the PlatformIO build (pio run first).
LVGL_DIR ?= .pio/libdeps/mava_handheld_s3/lvgl
LV_FLAGS := -DLV_CONF_SKIP -DLV_COLOR_DEPTH=16 -DLV_USE_BIDI=1 -DLV_USE_ARABIC_PERSIAN_CHARS=1 \
  -DLV_FONT_MONTSERRAT_14=1 -DLV_USE_QRCODE=1 \
  -DLV_USE_STDLIB_MALLOC=1 -DLV_USE_STDLIB_STRING=1 -DLV_USE_STDLIB_SPRINTF=1 -DLV_USE_ILI9341=0
UI_INC := -Isim/ui -Isrc/ui -Isrc -Ilib/meshkeys/src -Ilib/meshui/src -I$(LVGL_DIR) -I$(LVGL_DIR)/src
LV_SRC = $(shell find $(LVGL_DIR)/src -name '*.c' 2>/dev/null)
LV_OBJ = $(patsubst $(LVGL_DIR)/%.c,$(BUILD)/uisim/lvgl/%.o,$(LV_SRC))
UI_CXX := src/ui/ui.cpp src/ui/messenger.cpp lib/meshkeys/src/keys.cpp lib/meshkeys/src/kbproto.cpp \
  lib/meshui/src/osk.cpp sim/ui/sim_hw.cpp sim/ui/sim_main.cpp
UI_OBJ := $(UI_CXX:%.cpp=$(BUILD)/uisim/%.o) $(BUILD)/uisim/src/ui/fonts/mava_font_16.o

.PHONY: uishots lvgl-patched
lvgl-patched:
	python3 scripts/patch_lvgl.py .

uishots: lvgl-patched
	$(MAKE) $(BUILD)/uisim/uisim
	mkdir -p $(BUILD)/uishots && cd $(BUILD)/uishots && ../uisim/uisim

$(BUILD)/uisim/uisim: $(LV_OBJ) $(UI_OBJ)
	$(CXX) -o $@ $^

$(BUILD)/uisim/lvgl/%.o: $(LVGL_DIR)/%.c
	@mkdir -p $(dir $@)
	@$(CC) -O1 -w $(LV_FLAGS) -I$(LVGL_DIR) -c -o $@ $<

$(BUILD)/uisim/src/ui/fonts/%.o: src/ui/fonts/%.c
	@mkdir -p $(dir $@)
	$(CC) -O1 -w $(LV_FLAGS) -I$(LVGL_DIR) -c -o $@ $<

$(BUILD)/uisim/%.o: %.cpp
	@mkdir -p $(dir $@)
	$(CXX) -std=c++17 -O1 -Wall -Wextra $(LV_FLAGS) $(UI_INC) -c -o $@ $<
