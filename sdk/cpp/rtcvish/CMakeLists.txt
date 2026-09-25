cmake_minimum_required(VERSION 3.16)

if(CMAKE_SOURCE_DIR STREQUAL CMAKE_CURRENT_SOURCE_DIR)
    project(rtcvish C CXX)
endif()

find_package(Threads REQUIRED)

add_library(rtcvish STATIC
    Scheduler.cpp
    Server.cpp
    Wire.cpp
    emulator.pb.c
    nanopb/pb_common.c
    nanopb/pb_decode.c
    nanopb/pb_encode.c
)

target_compile_features(rtcvish PUBLIC cxx_std_17)
set_target_properties(rtcvish PROPERTIES CXX_EXTENSIONS OFF POSITION_INDEPENDENT_CODE ON)

# Consumers include "rtcvish/Server.h" etc.; nanopb stays private.
target_include_directories(rtcvish
    PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/..
    PRIVATE ${CMAKE_CURRENT_SOURCE_DIR} ${CMAKE_CURRENT_SOURCE_DIR}/nanopb
)
target_compile_definitions(rtcvish PRIVATE PB_ENABLE_MALLOC PB_FIELD_32BIT)
target_link_libraries(rtcvish PUBLIC Threads::Threads)
if(WIN32)
    target_link_libraries(rtcvish PUBLIC ws2_32)
endif()
