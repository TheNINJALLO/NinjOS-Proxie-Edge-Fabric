cmake_minimum_required(VERSION 3.20)
project(ninjos_edge_fabric VERSION 7.3.15 LANGUAGES CXX)

option(NINJOS_STATIC_LINK "Build a fully static gateway binary" ON)

add_executable(NinjOSEdge gateway/gateway.cpp gateway/product.hpp)
target_compile_features(NinjOSEdge PRIVATE cxx_std_20)
target_include_directories(NinjOSEdge PRIVATE gateway)
target_compile_options(NinjOSEdge PRIVATE -Wall -Wextra -Wpedantic)
target_link_libraries(NinjOSEdge PRIVATE pthread)

if(NINJOS_STATIC_LINK AND UNIX AND NOT APPLE)
    target_link_options(NinjOSEdge PRIVATE -static)
endif()

install(TARGETS NinjOSEdge RUNTIME DESTINATION bin)
