cmake_minimum_required(VERSION 3.20)

project(
    ninjos_proxie_companion
    VERSION 3.6.1
    LANGUAGES CXX
)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

set(
    ENDSTONE_API_VERSION
    "0.11.6"
    CACHE STRING
    "Exact Endstone release to compile against, without the leading v"
)

include(FetchContent)

FetchContent_Declare(
    endstone
    GIT_REPOSITORY https://github.com/EndstoneMC/endstone.git
    GIT_TAG v${ENDSTONE_API_VERSION}
    GIT_SHALLOW TRUE
)

FetchContent_MakeAvailable(endstone)

find_package(Threads REQUIRED)

endstone_add_plugin(
    ${PROJECT_NAME}
    src/plugin.cpp
)

target_link_libraries(
    ${PROJECT_NAME}
    PRIVATE
    Threads::Threads
)

if(CMAKE_CXX_COMPILER_ID MATCHES "Clang|GNU")
    target_compile_options(
        ${PROJECT_NAME}
        PRIVATE
        -Wall
        -Wextra
        -Wpedantic
    )
endif()

# Produce an install-ready Linux filename instead of lib<name>.so.
set_target_properties(
    ${PROJECT_NAME}
    PROPERTIES
    OUTPUT_NAME "ninjos_proxie_companion"
    PREFIX ""
)

install(
    TARGETS ${PROJECT_NAME}
    LIBRARY DESTINATION plugins
    RUNTIME DESTINATION plugins
)
