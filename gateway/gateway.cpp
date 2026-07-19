// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

#define _GNU_SOURCE

#include <arpa/inet.h>
#include <netdb.h>
#include <fcntl.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <unistd.h>

#include <algorithm>
#include <array>
#include <atomic>
#include <cerrno>
#include <cctype>
#include <chrono>
#include <csignal>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <deque>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <limits>
#include <map>
#include <optional>
#include <random>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>
#include <unordered_map>
#include <unordered_set>
#include <utility>
#include <vector>

#include "product.hpp"

namespace fs = std::filesystem;
using Clock = std::chrono::steady_clock;
using SystemClock = std::chrono::system_clock;

namespace {

std::atomic_bool g_running{true};
std::atomic_bool g_reload_requested{false};

void handle_signal(int signal) {
    if (signal == SIGHUP) {
        g_reload_requested.store(true);
        return;
    }
    g_running.store(false);
}

std::uint64_t epoch_ms() {
    return static_cast<std::uint64_t>(
        std::chrono::duration_cast<std::chrono::milliseconds>(
            SystemClock::now().time_since_epoch())
            .count());
}

std::uint64_t steady_ms() {
    return static_cast<std::uint64_t>(
        std::chrono::duration_cast<std::chrono::milliseconds>(
            Clock::now().time_since_epoch())
            .count());
}

std::string trim(std::string value) {
    auto not_space = [](unsigned char c) { return !std::isspace(c); };
    value.erase(value.begin(), std::find_if(value.begin(), value.end(), not_space));
    value.erase(std::find_if(value.rbegin(), value.rend(), not_space).base(), value.end());
    return value;
}

std::vector<std::string> split(std::string_view value, char delimiter) {
    std::vector<std::string> result;
    std::size_t start = 0;
    while (start <= value.size()) {
        const auto pos = value.find(delimiter, start);
        const auto end = pos == std::string_view::npos ? value.size() : pos;
        result.emplace_back(value.substr(start, end - start));
        if (pos == std::string_view::npos) {
            break;
        }
        start = pos + 1;
    }
    return result;
}

std::string json_escape(std::string_view value) {
    std::ostringstream out;
    for (unsigned char c : value) {
        switch (c) {
            case '\\': out << "\\\\"; break;
            case '"': out << "\\\""; break;
            case '\b': out << "\\b"; break;
            case '\f': out << "\\f"; break;
            case '\n': out << "\\n"; break;
            case '\r': out << "\\r"; break;
            case '\t': out << "\\t"; break;
            default:
                if (c < 0x20) {
                    out << "\\u" << std::hex << std::setw(4) << std::setfill('0')
                        << static_cast<int>(c) << std::dec;
                } else {
                    out << static_cast<char>(c);
                }
        }
    }
    return out.str();
}

std::string hex_preview(const std::byte* data, std::size_t size, std::size_t limit) {
    const std::size_t count = std::min(size, limit);
    std::ostringstream out;
    out << std::hex << std::setfill('0');
    for (std::size_t i = 0; i < count; ++i) {
        if (i != 0) {
            out << ' ';
        }
        out << std::setw(2) << static_cast<unsigned>(std::to_integer<unsigned char>(data[i]));
    }
    return out.str();
}

bool set_nonblocking(int fd) {
    const int flags = fcntl(fd, F_GETFL, 0);
    return flags >= 0 && fcntl(fd, F_SETFL, flags | O_NONBLOCK) == 0;
}

std::string sockaddr_host(const sockaddr_storage& address) {
    char host[NI_MAXHOST]{};
    const socklen_t length = address.ss_family == AF_INET
                                 ? static_cast<socklen_t>(sizeof(sockaddr_in))
                                 : static_cast<socklen_t>(sizeof(sockaddr_in6));
    if (getnameinfo(reinterpret_cast<const sockaddr*>(&address), length, host, sizeof(host),
                    nullptr, 0, NI_NUMERICHOST) != 0) {
        return "unknown";
    }
    return host;
}

std::uint16_t sockaddr_port(const sockaddr_storage& address) {
    if (address.ss_family == AF_INET) {
        return ntohs(reinterpret_cast<const sockaddr_in*>(&address)->sin_port);
    }
    if (address.ss_family == AF_INET6) {
        return ntohs(reinterpret_cast<const sockaddr_in6*>(&address)->sin6_port);
    }
    return 0;
}

std::string endpoint_string(const sockaddr_storage& address) {
    const auto host = sockaddr_host(address);
    const auto port = sockaddr_port(address);
    if (address.ss_family == AF_INET6) {
        return "[" + host + "]:" + std::to_string(port);
    }
    return host + ":" + std::to_string(port);
}

struct EndpointKey {
    int family{};
    std::array<unsigned char, 16> address{};
    std::uint16_t port{};
    std::uint16_t listener_port{};
    std::uint32_t scope{};

    static EndpointKey from(const sockaddr_storage& storage, std::uint16_t listener_port = 0) {
        EndpointKey key{};
        key.listener_port = listener_port;
        key.family = storage.ss_family;
        if (storage.ss_family == AF_INET) {
            const auto* in = reinterpret_cast<const sockaddr_in*>(&storage);
            std::memcpy(key.address.data(), &in->sin_addr, 4);
            key.port = in->sin_port;
        } else if (storage.ss_family == AF_INET6) {
            const auto* in6 = reinterpret_cast<const sockaddr_in6*>(&storage);
            std::memcpy(key.address.data(), &in6->sin6_addr, 16);
            key.port = in6->sin6_port;
            key.scope = in6->sin6_scope_id;
        }
        return key;
    }

    bool operator==(const EndpointKey& other) const noexcept {
        return family == other.family && address == other.address && port == other.port &&
               listener_port == other.listener_port && scope == other.scope;
    }
};

struct EndpointHash {
    std::size_t operator()(const EndpointKey& key) const noexcept {
        std::size_t hash = static_cast<std::size_t>(key.family);
        hash ^= static_cast<std::size_t>(key.port) << 16U;
        hash ^= static_cast<std::size_t>(key.listener_port) << 1U;
        for (auto byte : key.address) {
            hash = (hash * 131U) ^ byte;
        }
        return (hash * 131U) ^ key.scope;
    }
};

struct IpKey {
    int family{};
    std::array<unsigned char, 16> address{};
    std::uint32_t scope{};

    static IpKey from(const sockaddr_storage& storage) {
        IpKey key{};
        key.family = storage.ss_family;
        if (storage.ss_family == AF_INET) {
            const auto* in = reinterpret_cast<const sockaddr_in*>(&storage);
            std::memcpy(key.address.data(), &in->sin_addr, 4);
        } else if (storage.ss_family == AF_INET6) {
            const auto* in6 = reinterpret_cast<const sockaddr_in6*>(&storage);
            if (IN6_IS_ADDR_V4MAPPED(&in6->sin6_addr)) {
                key.family = AF_INET;
                std::memcpy(key.address.data(),
                            reinterpret_cast<const unsigned char*>(&in6->sin6_addr) + 12, 4);
            } else {
                std::memcpy(key.address.data(), &in6->sin6_addr, 16);
                key.scope = in6->sin6_scope_id;
            }
        }
        return key;
    }

    bool operator==(const IpKey& other) const noexcept {
        return family == other.family && address == other.address && scope == other.scope;
    }
};

struct IpHash {
    std::size_t operator()(const IpKey& key) const noexcept {
        std::size_t hash = static_cast<std::size_t>(key.family);
        for (auto byte : key.address) {
            hash = (hash * 131U) ^ byte;
        }
        return (hash * 131U) ^ key.scope;
    }
};

std::string ip_string(const IpKey& key) {
    char buffer[INET6_ADDRSTRLEN]{};
    if (key.family == AF_INET) {
        inet_ntop(AF_INET, key.address.data(), buffer, sizeof(buffer));
    } else if (key.family == AF_INET6) {
        inet_ntop(AF_INET6, key.address.data(), buffer, sizeof(buffer));
    } else {
        return "unknown";
    }
    return buffer;
}

struct ResolvedEndpoint {
    sockaddr_storage address{};
    socklen_t length{};
};

ResolvedEndpoint resolve_udp(const std::string& host, std::uint16_t port) {
    addrinfo hints{};
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_DGRAM;
    hints.ai_protocol = IPPROTO_UDP;

    addrinfo* results = nullptr;
    const std::string port_string = std::to_string(port);
    const int rc = getaddrinfo(host.c_str(), port_string.c_str(), &hints, &results);
    if (rc != 0 || results == nullptr) {
        throw std::runtime_error("Unable to resolve " + host + ": " + gai_strerror(rc));
    }

    ResolvedEndpoint endpoint{};
    std::memcpy(&endpoint.address, results->ai_addr, results->ai_addrlen);
    endpoint.length = static_cast<socklen_t>(results->ai_addrlen);
    freeaddrinfo(results);
    return endpoint;
}

std::uint64_t parse_unsigned(const std::string& value, std::uint64_t fallback) {
    try {
        std::size_t used = 0;
        const auto parsed = std::stoull(value, &used, 10);
        return used == value.size() ? parsed : fallback;
    } catch (...) {
        return fallback;
    }
}

bool parse_bool(const std::string& value, bool fallback) {
    std::string lower;
    lower.reserve(value.size());
    for (unsigned char c : value) {
        lower.push_back(static_cast<char>(std::tolower(c)));
    }
    if (lower == "true" || lower == "1" || lower == "yes" || lower == "on") {
        return true;
    }
    if (lower == "false" || lower == "0" || lower == "no" || lower == "off") {
        return false;
    }
    return fallback;
}

std::vector<int> parse_integer_list(const std::string& raw,
                                    const std::vector<int>& fallback) {
    std::vector<int> result;
    for (const auto& field : split(raw, ',')) {
        const auto value = parse_unsigned(trim(field), 0);
        if (value > 0 && value <= static_cast<std::uint64_t>(std::numeric_limits<int>::max())) {
            result.push_back(static_cast<int>(value));
        }
    }
    return result.empty() ? fallback : result;
}


std::map<std::string, std::string> load_properties(const fs::path& path) {
    std::ifstream input(path);
    if (!input) {
        throw std::runtime_error("Unable to open configuration: " + path.string());
    }

    std::map<std::string, std::string> values;
    std::string line;
    while (std::getline(input, line)) {
        line = trim(line);
        if (line.empty() || line[0] == '#') {
            continue;
        }
        const auto equals = line.find('=');
        if (equals == std::string::npos) {
            continue;
        }
        values[trim(line.substr(0, equals))] = trim(line.substr(equals + 1));
    }
    return values;
}

std::string property(const std::map<std::string, std::string>& values,
                     const std::string& name,
                     const std::string& fallback) {
    const auto found = values.find(name);
    return found == values.end() ? fallback : found->second;
}

struct BackendConfig {
    std::string name;
    std::string host;
    std::uint16_t port{};
    bool fallback{};
    std::string protection_profile;
    bool enabled{true};
};

struct StaticRouteConfig {
    std::uint16_t listener_port{};
    std::string backend_name;
};

struct ProtectionProfile {
    std::string name{"default"};
    std::size_t max_datagram_size{2048};
    std::size_t max_packets_per_second_per_ip{6000};
    std::size_t max_handshakes_per_minute{30};
    std::size_t max_sessions_per_ip{4};
    bool allow_new_sessions_during_incident{false};
};

struct Config {
    std::uint16_t listen_port{25566};
    std::string runtime_dir{"runtime"};
    std::string topology_file{"runtime/topology.properties"};
    std::vector<BackendConfig> backends;
    std::vector<StaticRouteConfig> static_routes;
    std::string primary_backend{"kingdom"};
    std::string routing_mode{"failover"};

    bool transfer_enabled{true};
    std::uint16_t transfer_port_start{25600};
    std::uint16_t transfer_port_end{25619};
    std::string transfer_ticket_file{"runtime/transfer-tickets.tsv"};
    int transfer_ticket_reload_ms{200};
    bool transfer_require_source_ip{true};

    int idle_timeout_seconds{45};
    int handshake_timeout_seconds{12};
    int cleanup_interval_seconds{5};
    std::size_t max_sessions{512};
    std::size_t max_sessions_per_ip{4};

    bool firewall_enabled{true};
    bool adaptive_firewall_enabled{true};
    int risk_decay_per_minute{5};
    int risk_warning_threshold{40};
    int risk_ban_threshold{100};
    std::vector<int> progressive_ban_seconds{30, 300, 3600, 86400};
    std::string firewall_allowlist_file{"runtime/firewall-allowlist.txt"};
    std::string firewall_denylist_file{"runtime/firewall-denylist.txt"};
    std::string firewall_bans_file{"runtime/firewall-bans.tsv"};
    std::string protection_profiles_file{"runtime/protection-profiles.properties"};
    std::size_t max_datagram_size{2048};
    std::size_t max_packets_per_second_per_ip{6000};
    std::size_t global_packets_per_second{250000};
    std::size_t max_handshakes_per_minute{30};
    std::size_t strike_limit{10};
    int temp_ban_seconds{300};

    bool ping_cache_enabled{true};
    int ping_cache_refresh_seconds{3};
    std::size_t ping_limit_per_second_per_ip{15};
    std::string maintenance_motd{"The Kingdom | Maintenance"};
    std::string drain_motd{"The Kingdom | Restarting Soon"};

    bool health_enabled{true};
    int health_interval_seconds{5};
    int health_timeout_ms{1500};
    int health_failure_threshold{3};
    int health_recovery_threshold{2};

    bool incident_mode_enabled{true};
    std::size_t incident_trigger_packets_per_second{180000};
    double incident_trigger_drop_ratio{0.20};
    std::size_t incident_min_packets_per_second{5000};
    int incident_recovery_seconds{60};
    int incident_rate_divisor{3};
    int incident_handshake_divisor{3};

    bool packet_capture_enabled{true};
    bool capture_outgoing{true};
    std::size_t packet_hex_preview_bytes{48};
    std::size_t packet_log_max_bytes{50ULL * 1024ULL * 1024ULL};
    std::size_t event_log_max_bytes{10ULL * 1024ULL * 1024ULL};

    int socket_receive_buffer{8 * 1024 * 1024};
    int socket_send_buffer{8 * 1024 * 1024};
    int stats_interval_seconds{30};
    int state_interval_ms{1000};
    int command_poll_ms{500};
    std::string live_config_file{"runtime/live-config.properties"};
    int live_config_reload_ms{1000};
};

std::vector<BackendConfig> parse_backends(const std::string& raw) {
    std::vector<BackendConfig> result;
    for (const auto& item_raw : split(raw, ';')) {
        const auto item = trim(item_raw);
        if (item.empty()) {
            continue;
        }
        const auto fields = split(item, '|');
        if (fields.size() < 3) {
            throw std::runtime_error(
                "Invalid backend entry. Expected name|host|port|fallback: " + item);
        }
        const auto port_value = parse_unsigned(fields[2], 0);
        if (port_value == 0 || port_value > 65535) {
            throw std::runtime_error("Invalid backend port in: " + item);
        }
        BackendConfig backend{};
        backend.name = trim(fields[0]);
        backend.host = trim(fields[1]);
        backend.port = static_cast<std::uint16_t>(port_value);
        backend.fallback = fields.size() >= 4 ? parse_bool(trim(fields[3]), false) : false;
        backend.protection_profile =
            fields.size() >= 5 && !trim(fields[4]).empty() ? trim(fields[4]) : backend.name;
        backend.enabled = fields.size() >= 6 ? parse_bool(trim(fields[5]), true) : true;
        if (backend.name.empty() || backend.host.empty()) {
            throw std::runtime_error("Backend name and host are required: " + item);
        }
        result.push_back(std::move(backend));
    }
    if (result.empty()) {
        throw std::runtime_error("At least one backend is required.");
    }
    return result;
}

std::vector<StaticRouteConfig> parse_static_routes(const std::string& raw) {
    std::vector<StaticRouteConfig> result;
    std::unordered_set<std::uint16_t> used_ports;

    for (const auto& item_raw : split(raw, ';')) {
        const auto item = trim(item_raw);
        if (item.empty()) {
            continue;
        }

        const auto fields = split(item, '|');
        if (fields.size() != 2) {
            throw std::runtime_error(
                "Invalid static route. Expected listenerPort|backendName: " + item);
        }

        const auto port_value = parse_unsigned(trim(fields[0]), 0);
        const auto backend_name = trim(fields[1]);
        if (port_value == 0 || port_value > 65535 || backend_name.empty()) {
            throw std::runtime_error("Invalid static route: " + item);
        }

        const auto port = static_cast<std::uint16_t>(port_value);
        if (!used_ports.insert(port).second) {
            throw std::runtime_error(
                "Static route listener port is configured more than once: " +
                std::to_string(port));
        }

        result.push_back(StaticRouteConfig{port, backend_name});
    }

    return result;
}


Config load_config(const fs::path& path) {
    const auto values = load_properties(path);
    Config cfg{};
    cfg.listen_port = static_cast<std::uint16_t>(parse_unsigned(property(values, "listen_port", "25566"), 25566));
    cfg.runtime_dir = property(values, "runtime_dir", "runtime");
    cfg.topology_file = property(values, "topology_file", cfg.runtime_dir + "/topology.properties");

    auto topology_values = values;
    std::error_code topology_error;
    if (fs::exists(cfg.topology_file, topology_error)) {
        const auto managed = load_properties(cfg.topology_file);
        for (const auto& [key, value] : managed) {
            topology_values[key] = value;
        }
    }

    cfg.backends = parse_backends(property(topology_values, "backends", "kingdom|127.0.0.1|25565|false|kingdom|true"));
    cfg.static_routes = parse_static_routes(
        property(topology_values, "static_routes", "25566|kingdom"));
    cfg.primary_backend = property(topology_values, "primary_backend", cfg.backends.front().name);
    cfg.routing_mode = property(topology_values, "routing_mode", "failover");

    cfg.transfer_enabled = parse_bool(property(values, "transfer_enabled", "true"), true);
    cfg.transfer_port_start = static_cast<std::uint16_t>(parse_unsigned(property(values, "transfer_port_start", "25600"), 25600));
    cfg.transfer_port_end = static_cast<std::uint16_t>(parse_unsigned(property(values, "transfer_port_end", "25619"), 25619));
    cfg.transfer_ticket_file = property(values, "transfer_ticket_file", "runtime/transfer-tickets.tsv");
    cfg.transfer_ticket_reload_ms = static_cast<int>(parse_unsigned(property(values, "transfer_ticket_reload_ms", "200"), 200));
    cfg.transfer_require_source_ip = parse_bool(property(values, "transfer_require_source_ip", "true"), true);
    if (cfg.transfer_port_end < cfg.transfer_port_start) {
        std::swap(cfg.transfer_port_start, cfg.transfer_port_end);
    }

    cfg.idle_timeout_seconds = static_cast<int>(parse_unsigned(property(values, "idle_timeout_seconds", "45"), 45));
    cfg.handshake_timeout_seconds = static_cast<int>(parse_unsigned(property(values, "handshake_timeout_seconds", "12"), 12));
    cfg.cleanup_interval_seconds = static_cast<int>(parse_unsigned(property(values, "cleanup_interval_seconds", "5"), 5));
    cfg.max_sessions = parse_unsigned(property(values, "max_sessions", "512"), 512);
    cfg.max_sessions_per_ip = parse_unsigned(property(values, "max_sessions_per_ip", "4"), 4);

    cfg.firewall_enabled = parse_bool(property(values, "firewall_enabled", "true"), true);
    cfg.adaptive_firewall_enabled = parse_bool(property(values, "adaptive_firewall_enabled", "true"), true);
    cfg.risk_decay_per_minute = static_cast<int>(parse_unsigned(property(values, "risk_decay_per_minute", "5"), 5));
    cfg.risk_warning_threshold = static_cast<int>(parse_unsigned(property(values, "risk_warning_threshold", "40"), 40));
    cfg.risk_ban_threshold = static_cast<int>(parse_unsigned(property(values, "risk_ban_threshold", "100"), 100));
    cfg.progressive_ban_seconds = parse_integer_list(
        property(values, "progressive_ban_seconds", "30,300,3600,86400"),
        {30, 300, 3600, 86400});
    cfg.firewall_allowlist_file = property(values, "firewall_allowlist_file", "runtime/firewall-allowlist.txt");
    cfg.firewall_denylist_file = property(values, "firewall_denylist_file", "runtime/firewall-denylist.txt");
    cfg.firewall_bans_file = property(values, "firewall_bans_file", "runtime/firewall-bans.tsv");
    cfg.protection_profiles_file =
        property(values, "protection_profiles_file", "runtime/protection-profiles.properties");
    cfg.max_datagram_size = parse_unsigned(property(values, "max_datagram_size", "2048"), 2048);
    cfg.max_packets_per_second_per_ip = parse_unsigned(property(values, "max_packets_per_second_per_ip", "6000"), 6000);
    cfg.global_packets_per_second = parse_unsigned(property(values, "global_packets_per_second", "250000"), 250000);
    cfg.max_handshakes_per_minute = parse_unsigned(property(values, "max_handshakes_per_minute", "30"), 30);
    cfg.strike_limit = parse_unsigned(property(values, "strike_limit", "10"), 10);
    cfg.temp_ban_seconds = static_cast<int>(parse_unsigned(property(values, "temp_ban_seconds", "300"), 300));

    cfg.ping_cache_enabled = parse_bool(property(values, "ping_cache_enabled", "true"), true);
    cfg.ping_cache_refresh_seconds = static_cast<int>(parse_unsigned(property(values, "ping_cache_refresh_seconds", "3"), 3));
    cfg.ping_limit_per_second_per_ip = parse_unsigned(property(values, "ping_limit_per_second_per_ip", "15"), 15);
    cfg.maintenance_motd = property(values, "maintenance_motd", cfg.maintenance_motd);
    cfg.drain_motd = property(values, "drain_motd", cfg.drain_motd);

    cfg.health_enabled = parse_bool(property(values, "health_enabled", "true"), true);
    cfg.health_interval_seconds = static_cast<int>(parse_unsigned(property(values, "health_interval_seconds", "5"), 5));
    cfg.health_timeout_ms = static_cast<int>(parse_unsigned(property(values, "health_timeout_ms", "1500"), 1500));
    cfg.health_failure_threshold = static_cast<int>(parse_unsigned(property(values, "health_failure_threshold", "3"), 3));
    cfg.health_recovery_threshold = static_cast<int>(parse_unsigned(property(values, "health_recovery_threshold", "2"), 2));

    cfg.incident_mode_enabled =
        parse_bool(property(values, "incident_mode_enabled", "true"), true);
    cfg.incident_trigger_packets_per_second =
        parse_unsigned(property(values, "incident_trigger_packets_per_second", "180000"), 180000);
    try {
        cfg.incident_trigger_drop_ratio =
            std::stod(property(values, "incident_trigger_drop_ratio", "0.20"));
    } catch (...) {
        cfg.incident_trigger_drop_ratio = 0.20;
    }
    cfg.incident_trigger_drop_ratio =
        std::clamp(cfg.incident_trigger_drop_ratio, 0.01, 1.0);
    cfg.incident_min_packets_per_second =
        parse_unsigned(property(values, "incident_min_packets_per_second", "5000"), 5000);
    cfg.incident_recovery_seconds =
        static_cast<int>(parse_unsigned(property(values, "incident_recovery_seconds", "60"), 60));
    cfg.incident_rate_divisor =
        static_cast<int>(std::max<std::size_t>(1, parse_unsigned(
            property(values, "incident_rate_divisor", "3"), 3)));
    cfg.incident_handshake_divisor =
        static_cast<int>(std::max<std::size_t>(1, parse_unsigned(
            property(values, "incident_handshake_divisor", "3"), 3)));

    cfg.packet_capture_enabled = parse_bool(property(values, "packet_capture_enabled", "true"), true);
    cfg.capture_outgoing = parse_bool(property(values, "capture_outgoing", "true"), true);
    cfg.packet_hex_preview_bytes = parse_unsigned(property(values, "packet_hex_preview_bytes", "48"), 48);
    cfg.packet_log_max_bytes = parse_unsigned(property(values, "packet_log_max_bytes", "52428800"), 52428800);
    cfg.event_log_max_bytes = parse_unsigned(property(values, "event_log_max_bytes", "10485760"), 10485760);

    cfg.socket_receive_buffer = static_cast<int>(parse_unsigned(property(values, "socket_receive_buffer", "8388608"), 8388608));
    cfg.socket_send_buffer = static_cast<int>(parse_unsigned(property(values, "socket_send_buffer", "8388608"), 8388608));
    cfg.stats_interval_seconds = static_cast<int>(parse_unsigned(property(values, "stats_interval_seconds", "30"), 30));
    cfg.state_interval_ms = static_cast<int>(parse_unsigned(property(values, "state_interval_ms", "1000"), 1000));
    cfg.command_poll_ms = static_cast<int>(parse_unsigned(property(values, "command_poll_ms", "500"), 500));
    cfg.live_config_file = property(values, "live_config_file", "runtime/live-config.properties");
    cfg.live_config_reload_ms = static_cast<int>(parse_unsigned(property(values, "live_config_reload_ms", "1000"), 1000));

    cfg.risk_warning_threshold = std::max(1, cfg.risk_warning_threshold);
    cfg.risk_ban_threshold = std::max(cfg.risk_warning_threshold + 1, cfg.risk_ban_threshold);
    cfg.risk_decay_per_minute = std::max(0, cfg.risk_decay_per_minute);

    std::unordered_set<std::string> backend_names;
    for (const auto& backend : cfg.backends) {
        if (!backend_names.insert(backend.name).second) {
            throw std::runtime_error("Backend name is configured more than once: " + backend.name);
        }
    }
    if (!backend_names.contains(cfg.primary_backend)) {
        throw std::runtime_error("Primary backend does not exist: " + cfg.primary_backend);
    }
    for (const auto& route : cfg.static_routes) {
        if (!backend_names.contains(route.backend_name)) {
            throw std::runtime_error("Static route references unknown backend: " + route.backend_name);
        }
    }
    return cfg;
}

class RotatingJsonl {
public:
    RotatingJsonl(fs::path path, std::size_t max_bytes)
        : path_(std::move(path)), max_bytes_(std::max<std::size_t>(max_bytes, 1024)) {
        fs::create_directories(path_.parent_path());
        open();
    }

    void write(const std::string& line) {
        if (!stream_) {
            open();
        }
        stream_ << line << '\n';
        bytes_ += line.size() + 1;
        if (++pending_flush_ >= 32) {
            stream_.flush();
            pending_flush_ = 0;
        }
        if (bytes_ >= max_bytes_) {
            rotate();
        }
    }

    void flush() {
        stream_.flush();
        pending_flush_ = 0;
    }

private:
    void open() {
        if (fs::exists(path_)) {
            std::error_code error;
            bytes_ = fs::file_size(path_, error);
            if (error) {
                bytes_ = 0;
            }
        }
        stream_.open(path_, std::ios::app);
    }

    void rotate() {
        stream_.flush();
        stream_.close();
        const fs::path previous = path_.string() + ".1";
        std::error_code error;
        fs::remove(previous, error);
        error.clear();
        fs::rename(path_, previous, error);
        bytes_ = 0;
        pending_flush_ = 0;
        open();
    }

    fs::path path_;
    std::size_t max_bytes_{};
    std::size_t bytes_{};
    std::size_t pending_flush_{};
    std::ofstream stream_;
};

std::string raknet_name(unsigned char id) {
    switch (id) {
        case 0x00: return "ConnectedPing";
        case 0x01: return "UnconnectedPing";
        case 0x02: return "UnconnectedPingOpenConnections";
        case 0x03: return "ConnectedPong";
        case 0x05: return "OpenConnectionRequest1";
        case 0x06: return "OpenConnectionReply1";
        case 0x07: return "OpenConnectionRequest2";
        case 0x08: return "OpenConnectionReply2";
        case 0x09: return "ConnectionRequest";
        case 0x10: return "ConnectionRequestAccepted";
        case 0x13: return "NewIncomingConnection";
        case 0x15: return "DisconnectNotification";
        case 0x1C: return "UnconnectedPong";
        case 0x19: return "IncompatibleProtocol";
        case 0xA0: return "NACK";
        case 0xC0: return "ACK";
        default:
            if (id >= 0x80 && id <= 0x8D) {
                return "FrameSet";
            }
            return "RakNetOrOpaque";
    }
}

bool is_unconnected_ping(unsigned char id) {
    return id == 0x01 || id == 0x02;
}

bool is_handshake_packet(unsigned char id) {
    return id == 0x05 || id == 0x07 || id == 0x09;
}

void write_be64(std::byte* output, std::uint64_t value) {
    for (int i = 7; i >= 0; --i) {
        output[7 - i] = static_cast<std::byte>((value >> (i * 8)) & 0xFFU);
    }
}

std::uint64_t read_be64(const std::byte* input) {
    std::uint64_t value = 0;
    for (int i = 0; i < 8; ++i) {
        value = (value << 8U) | std::to_integer<unsigned char>(input[i]);
    }
    return value;
}

constexpr std::array<std::byte, 16> kRakNetMagic{
    std::byte{0x00}, std::byte{0xFF}, std::byte{0xFF}, std::byte{0x00},
    std::byte{0xFE}, std::byte{0xFE}, std::byte{0xFE}, std::byte{0xFE},
    std::byte{0xFD}, std::byte{0xFD}, std::byte{0xFD}, std::byte{0xFD},
    std::byte{0x12}, std::byte{0x34}, std::byte{0x56}, std::byte{0x78}};

std::vector<std::byte> make_unconnected_ping(std::uint64_t token, std::uint64_t guid) {
    std::vector<std::byte> packet(33);
    packet[0] = std::byte{0x01};
    write_be64(packet.data() + 1, token);
    std::copy(kRakNetMagic.begin(), kRakNetMagic.end(), packet.begin() + 9);
    write_be64(packet.data() + 25, guid);
    return packet;
}

std::vector<std::byte> customize_pong(const std::vector<std::byte>& cached,
                                      const std::byte* request,
                                      std::size_t request_size,
                                      const std::string& motd_override) {
    if (cached.size() < 35 || request_size < 9) {
        return cached;
    }

    std::vector<std::byte> result = cached;
    std::copy(request + 1, request + 9, result.begin() + 1);

    if (motd_override.empty()) {
        return result;
    }

    const std::size_t length_offset = 33;
    const std::uint16_t old_length =
        static_cast<std::uint16_t>((std::to_integer<unsigned char>(result[length_offset]) << 8U) |
                                   std::to_integer<unsigned char>(result[length_offset + 1]));
    if (length_offset + 2 + old_length > result.size()) {
        return result;
    }

    std::string advertisement(reinterpret_cast<const char*>(result.data() + length_offset + 2),
                              old_length);
    auto fields = split(advertisement, ';');
    if (fields.size() >= 2) {
        fields[1] = motd_override;
    } else {
        advertisement = "MCPE;" + motd_override;
        fields = split(advertisement, ';');
    }

    std::ostringstream rebuilt;
    for (std::size_t index = 0; index < fields.size(); ++index) {
        if (index != 0) {
            rebuilt << ';';
        }
        rebuilt << fields[index];
    }
    const std::string text = rebuilt.str();
    if (text.size() > std::numeric_limits<std::uint16_t>::max()) {
        return result;
    }

    result.resize(length_offset + 2 + text.size());
    result[length_offset] = static_cast<std::byte>((text.size() >> 8U) & 0xFFU);
    result[length_offset + 1] = static_cast<std::byte>(text.size() & 0xFFU);
    std::memcpy(result.data() + length_offset + 2, text.data(), text.size());
    return result;
}

int create_listener(std::uint16_t port, int receive_buffer, int send_buffer) {
    const int fd = socket(AF_INET6, SOCK_DGRAM | SOCK_CLOEXEC, IPPROTO_UDP);
    if (fd < 0) {
        throw std::runtime_error("socket(listener): " + std::string(std::strerror(errno)));
    }

    int off = 0;
    int on = 1;
    setsockopt(fd, IPPROTO_IPV6, IPV6_V6ONLY, &off, sizeof(off));
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &on, sizeof(on));
    setsockopt(fd, SOL_SOCKET, SO_RCVBUF, &receive_buffer, sizeof(receive_buffer));
    setsockopt(fd, SOL_SOCKET, SO_SNDBUF, &send_buffer, sizeof(send_buffer));

    sockaddr_in6 address{};
    address.sin6_family = AF_INET6;
    address.sin6_addr = in6addr_any;
    address.sin6_port = htons(port);
    if (bind(fd, reinterpret_cast<sockaddr*>(&address), sizeof(address)) != 0) {
        const std::string message = std::strerror(errno);
        close(fd);
        throw std::runtime_error("bind(listener): " + message);
    }

    if (!set_nonblocking(fd)) {
        const std::string message = std::strerror(errno);
        close(fd);
        throw std::runtime_error("fcntl(listener): " + message);
    }
    return fd;
}

int create_connected_udp(const ResolvedEndpoint& endpoint, int receive_buffer, int send_buffer) {
    const int fd = socket(endpoint.address.ss_family, SOCK_DGRAM | SOCK_CLOEXEC, IPPROTO_UDP);
    if (fd < 0) {
        return -1;
    }
    setsockopt(fd, SOL_SOCKET, SO_RCVBUF, &receive_buffer, sizeof(receive_buffer));
    setsockopt(fd, SOL_SOCKET, SO_SNDBUF, &send_buffer, sizeof(send_buffer));
    if (!set_nonblocking(fd)) {
        close(fd);
        return -1;
    }
    if (connect(fd, reinterpret_cast<const sockaddr*>(&endpoint.address), endpoint.length) != 0 &&
        errno != EINPROGRESS) {
        close(fd);
        return -1;
    }
    return fd;
}

struct Backend {
    BackendConfig config;
    ResolvedEndpoint endpoint;
    int health_fd{-1};
    bool manual_enabled{true};
    bool healthy{true};
    bool awaiting_health_response{false};
    int failure_count{};
    int recovery_count{};
    double latency_ms{};
    std::uint64_t ping_token{};
    Clock::time_point ping_sent{};
    Clock::time_point last_ping_attempt{};
    Clock::time_point last_response{};
    std::vector<std::byte> cached_pong;
    std::size_t active_sessions{};
};

struct TransferTicket {
    std::string id;
    std::uint16_t port{};
    std::size_t backend_index{};
    std::string backend_name;
    std::string source_ip;
    std::string xuid;
    std::string player;
    std::string source_server;
    std::uint64_t expires_at_ms{};
    bool consumed{};
};

struct TicketRoute {
    std::size_t backend_index{};
    std::string ticket_id;
};

struct Session {
    int fd{-1};
    int listener_fd{-1};
    std::uint16_t listener_port{};
    std::uint16_t upstream_local_port{};
    std::string transfer_ticket_id;
    sockaddr_storage client{};
    socklen_t client_length{};
    IpKey ip{};
    std::size_t backend_index{};
    Clock::time_point created{};
    Clock::time_point last_activity{};
    std::uint64_t client_to_server_bytes{};
    std::uint64_t server_to_client_bytes{};
    std::uint64_t client_packets{};
    std::uint64_t server_packets{};
};

struct IpState {
    Clock::time_point packet_window{};
    Clock::time_point ping_window{};
    Clock::time_point handshake_window{};
    std::size_t packets_in_window{};
    std::size_t pings_in_window{};
    std::size_t handshakes_in_window{};
    std::size_t strikes{};
    std::size_t active_sessions{};
    Clock::time_point banned_until{};
    std::uint64_t banned_until_epoch_ms{};
    int risk_score{};
    int ban_level{};
    std::uint64_t offenses{};
    Clock::time_point risk_updated{};
    std::string last_reason;
    std::uint64_t total_dropped{};
};

struct Counters {
    std::uint64_t client_packets{};
    std::uint64_t server_packets{};
    std::uint64_t client_bytes{};
    std::uint64_t server_bytes{};
    std::uint64_t dropped_packets{};
    std::uint64_t rate_limited{};
    std::uint64_t temporary_bans{};
    std::uint64_t cached_ping_replies{};
    std::uint64_t health_checks{};
    std::uint64_t health_failures{};
    std::uint64_t sessions_opened{};
    std::uint64_t sessions_closed{};
    std::uint64_t transfer_tickets_loaded{};
    std::uint64_t transfer_tickets_consumed{};
    std::uint64_t transfer_ticket_rejections{};
    std::uint64_t adaptive_warnings{};
    std::uint64_t persistent_bans_loaded{};
    std::uint64_t denylist_drops{};
    std::uint64_t config_reloads{};
    std::uint64_t config_reload_failures{};
    std::uint64_t incident_entries{};
    std::uint64_t incident_exits{};
};

class Gateway {
public:
    explicit Gateway(Config config, fs::path config_path)
        : config_(std::move(config)),
          base_config_path_(std::move(config_path)),
          runtime_dir_(config_.runtime_dir),
          transfer_ticket_path_(config_.transfer_ticket_file),
          live_config_path_(config_.live_config_file),
          firewall_allowlist_path_(config_.firewall_allowlist_file),
          firewall_denylist_path_(config_.firewall_denylist_file),
          firewall_bans_path_(config_.firewall_bans_file),
          protection_profiles_path_(config_.protection_profiles_file),
          packet_log_(runtime_dir_ / "transport-packets.jsonl", config_.packet_log_max_bytes),
          event_log_(runtime_dir_ / "events.jsonl", config_.event_log_max_bytes),
          start_time_(Clock::now()),
          last_cleanup_(start_time_),
          last_stats_(start_time_),
          last_state_(start_time_),
          last_command_poll_(start_time_),
          last_ticket_reload_(start_time_ - std::chrono::seconds(1)),
          last_live_config_check_(start_time_ - std::chrono::seconds(5)),
          last_incident_check_(start_time_),
          incident_last_change_(start_time_),
          global_window_(start_time_) {
        fs::create_directories(runtime_dir_);
        fs::create_directories(live_config_path_.parent_path());
        fs::create_directories(firewall_allowlist_path_.parent_path());
        fs::create_directories(firewall_denylist_path_.parent_path());
        fs::create_directories(firewall_bans_path_.parent_path());
        fs::create_directories(protection_profiles_path_.parent_path());
        ensure_policy_files();
        load_firewall_files();
        load_protection_profiles();
        load_persistent_bans();
        reload_live_config(true);
        command_path_ = runtime_dir_ / "commands.log";
        std::ofstream(command_path_, std::ios::app).close();
        initialize_backends();
        epoll_fd_ = epoll_create1(EPOLL_CLOEXEC);
        if (epoll_fd_ < 0) {
            throw std::runtime_error("epoll_create1: " + std::string(std::strerror(errno)));
        }
        add_listener(config_.listen_port);
        for (const auto& route : config_.static_routes) {
            add_listener(route.listener_port);
        }
        if (config_.transfer_enabled) {
            for (std::uint32_t port = config_.transfer_port_start;
                 port <= config_.transfer_port_end; ++port) {
                if (port != config_.listen_port) {
                    add_listener(static_cast<std::uint16_t>(port));
                }
            }
        }
        fs::create_directories(transfer_ticket_path_.parent_path());
        std::ofstream(transfer_ticket_path_, std::ios::app).close();
        refresh_transfer_tickets(Clock::now(), true);
        for (std::size_t index = 0; index < backends_.size(); ++index) {
            add_epoll(backends_[index].health_fd);
            health_fd_to_backend_[backends_[index].health_fd] = index;
        }
        load_command_offset();
        std::error_code pending_error;
        fs::remove(runtime_dir_ / "topology-restart.pending", pending_error);
    }

    ~Gateway() {
        shutdown();
    }

    int run() {
        std::cout << "[Ninj-OS Edge] Listening on [::]:" << config_.listen_port << "/UDP\n";
        std::cout << "[Ninj-OS Edge] Backends=" << backends_.size()
                  << " routing=" << config_.routing_mode << " primary=" << config_.primary_backend
                  << "\n";
        std::cout << "[Ninj-OS Edge] Transparent Microsoft identity remains end-to-end.\n";
        std::cout << "[Ninj-OS Proxie] Engine: " << ninjos::product::kEngine << "\n";
        std::cout << "[Ninj-OS Proxie] Implementation: " << ninjos::product::kImplementation << "\n";
        std::cout << "[Ninj-OS Proxie] Reference: " << ninjos::product::kReference << "\n";
        std::cout << "[Ninj-OS Proxie] Reference URL: " << ninjos::product::kReferenceUrl << "\n";
        log_event("gateway.started",
                  std::string(ninjos::product::kName) + " v" +
                      std::string(ninjos::product::kVersion) + " started",
                  "info");

        std::vector<epoll_event> events(512);
        while (g_running.load()) {
            const int ready = epoll_wait(epoll_fd_, events.data(), static_cast<int>(events.size()), 100);
            if (ready < 0) {
                if (errno == EINTR) {
                    continue;
                }
                std::cerr << "[Ninj-OS Edge] epoll_wait failed: " << std::strerror(errno) << "\n";
                break;
            }

            const auto now = Clock::now();
            for (int index = 0; index < ready; ++index) {
                const int fd = events[index].data.fd;
                if (const auto listener = listener_fd_to_port_.find(fd);
                    listener != listener_fd_to_port_.end()) {
                    handle_listener(fd, listener->second, now);
                    continue;
                }
                if (const auto health = health_fd_to_backend_.find(fd);
                    health != health_fd_to_backend_.end()) {
                    handle_health_response(health->second, now);
                    continue;
                }
                if ((events[index].events & (EPOLLERR | EPOLLHUP)) != 0) {
                    close_session(fd, "socket-error");
                    continue;
                }
                handle_backend_data(fd, now);
            }

            handle_health_checks(now);
            refresh_transfer_tickets(now, false);
            handle_commands(now);
            handle_live_config(now);
            handle_incident_mode(now);
            if (g_reload_requested.exchange(false)) {
                reload_live_config(true);
            }
            cleanup(now);
            write_state(now);
            print_stats(now);
        }

        log_event("gateway.stopping", "Ninj-OS Edge Gateway stopping", "warning");
        shutdown();
        return restart_requested_ ? restart_exit_code_ : 0;
    }

private:
    static std::string sanitize_policy_line(std::string line) {
        const auto comment = line.find('#');
        if (comment != std::string::npos) {
            line.erase(comment);
        }
        return trim(line);
    }

    void ensure_policy_files() {
        for (const auto& path : {firewall_allowlist_path_, firewall_denylist_path_,
                                 firewall_bans_path_, live_config_path_,
                                 protection_profiles_path_}) {
            std::ofstream(path, std::ios::app).close();
        }
        if (fs::file_size(live_config_path_) == 0) {
            std::ofstream output(live_config_path_, std::ios::trunc);
            output << "# Ninj-OS Edge Fabric live policy overrides\n"
                   << "# Changes are applied without dropping existing sessions.\n"
                   << "routing_mode=" << config_.routing_mode << "\n"
                   << "firewall_enabled=" << (config_.firewall_enabled ? "true" : "false") << "\n"
                   << "adaptive_firewall_enabled=" << (config_.adaptive_firewall_enabled ? "true" : "false") << "\n"
                   << "max_packets_per_second_per_ip=" << config_.max_packets_per_second_per_ip << "\n"
                   << "global_packets_per_second=" << config_.global_packets_per_second << "\n"
                   << "max_handshakes_per_minute=" << config_.max_handshakes_per_minute << "\n"
                   << "risk_decay_per_minute=" << config_.risk_decay_per_minute << "\n"
                   << "risk_warning_threshold=" << config_.risk_warning_threshold << "\n"
                   << "risk_ban_threshold=" << config_.risk_ban_threshold << "\n"
                   << "progressive_ban_seconds=30,300,3600,86400\n"
                   << "ping_limit_per_second_per_ip=" << config_.ping_limit_per_second_per_ip << "\n"
                   << "health_failure_threshold=" << config_.health_failure_threshold << "\n"
                   << "health_recovery_threshold=" << config_.health_recovery_threshold << "\n"
                   << "packet_capture_enabled=" << (config_.packet_capture_enabled ? "true" : "false") << "\n"
                   << "packet_hex_preview_bytes=" << config_.packet_hex_preview_bytes << "\n";
        }
    }


    void ensure_protection_profiles() {
        if (fs::file_size(protection_profiles_path_) != 0) {
            return;
        }
        std::ofstream output(protection_profiles_path_, std::ios::trunc);
        output << "# profile.key=value\n"
               << "# Backends use their name as the profile unless BACKENDS includes a fifth field.\n"
               << "default.max_datagram_size=" << config_.max_datagram_size << "\n"
               << "default.max_packets_per_second_per_ip="
               << config_.max_packets_per_second_per_ip << "\n"
               << "default.max_handshakes_per_minute="
               << config_.max_handshakes_per_minute << "\n"
               << "default.max_sessions_per_ip=" << config_.max_sessions_per_ip << "\n"
               << "default.allow_new_sessions_during_incident=false\n"
               << "kingdom.max_packets_per_second_per_ip=7000\n"
               << "kingdom.max_handshakes_per_minute=35\n"
               << "kingdom.max_sessions_per_ip=5\n"
               << "zoo.max_packets_per_second_per_ip=4000\n"
               << "zoo.max_handshakes_per_minute=20\n"
               << "zoo.max_sessions_per_ip=3\n";
    }

    void load_protection_profiles() {
        ensure_protection_profiles();
        protection_profiles_.clear();

        ProtectionProfile defaults{};
        defaults.name = "default";
        defaults.max_datagram_size = config_.max_datagram_size;
        defaults.max_packets_per_second_per_ip = config_.max_packets_per_second_per_ip;
        defaults.max_handshakes_per_minute = config_.max_handshakes_per_minute;
        defaults.max_sessions_per_ip = config_.max_sessions_per_ip;
        protection_profiles_["default"] = defaults;

        const auto values = load_properties(protection_profiles_path_);
        std::unordered_set<std::string> names{"default"};
        for (const auto& [key, value] : values) {
            (void)value;
            const auto dot = key.find('.');
            if (dot != std::string::npos && dot > 0) {
                names.insert(key.substr(0, dot));
            }
        }

        for (const auto& name : names) {
            ProtectionProfile profile = defaults;
            profile.name = name;
            const auto prefix = name + ".";
            profile.max_datagram_size = parse_unsigned(
                property(values, prefix + "max_datagram_size",
                         std::to_string(profile.max_datagram_size)),
                profile.max_datagram_size);
            profile.max_packets_per_second_per_ip = parse_unsigned(
                property(values, prefix + "max_packets_per_second_per_ip",
                         std::to_string(profile.max_packets_per_second_per_ip)),
                profile.max_packets_per_second_per_ip);
            profile.max_handshakes_per_minute = parse_unsigned(
                property(values, prefix + "max_handshakes_per_minute",
                         std::to_string(profile.max_handshakes_per_minute)),
                profile.max_handshakes_per_minute);
            profile.max_sessions_per_ip = parse_unsigned(
                property(values, prefix + "max_sessions_per_ip",
                         std::to_string(profile.max_sessions_per_ip)),
                profile.max_sessions_per_ip);
            profile.allow_new_sessions_during_incident = parse_bool(
                property(values, prefix + "allow_new_sessions_during_incident", "false"),
                false);
            protection_profiles_[name] = profile;
        }
    }


    void load_ip_file(const fs::path& path,
                      std::unordered_set<IpKey, IpHash>& target) {
        target.clear();
        std::ifstream input(path);
        std::string line;
        while (std::getline(input, line)) {
            line = sanitize_policy_line(std::move(line));
            if (line.empty()) continue;
            const auto key = parse_ip_key(line);
            if (key) target.insert(*key);
        }
    }

    void load_firewall_files() {
        load_ip_file(firewall_allowlist_path_, allowlisted_ips_);
        load_ip_file(firewall_denylist_path_, denylisted_ips_);
    }

    void load_persistent_bans() {
        std::ifstream input(firewall_bans_path_);
        std::string line;
        const auto now_epoch = epoch_ms();
        while (std::getline(input, line)) {
            line = sanitize_policy_line(std::move(line));
            if (line.empty()) continue;
            const auto fields = split(line, '\t');
            if (fields.size() < 5) continue;
            const auto key = parse_ip_key(fields[0]);
            if (!key) continue;
            auto& state = ip_states_[*key];
            initialize_ip_windows(state, Clock::now());
            state.banned_until_epoch_ms = parse_unsigned(fields[1], 0);
            state.ban_level = static_cast<int>(parse_unsigned(fields[2], 0));
            state.risk_score = static_cast<int>(parse_unsigned(fields[3], 0));
            state.offenses = parse_unsigned(fields[4], 0);
            state.last_reason = fields.size() >= 6 ? fields[5] : "persisted";
            if (state.banned_until_epoch_ms > now_epoch) {
                state.banned_until = Clock::now() + std::chrono::milliseconds(
                    state.banned_until_epoch_ms - now_epoch);
                counters_.persistent_bans_loaded++;
            } else {
                state.banned_until_epoch_ms = 0;
            }
        }
    }

    void save_persistent_bans() {
        const auto temporary = firewall_bans_path_.string() + ".tmp";
        std::ofstream output(temporary, std::ios::trunc);
        if (!output) return;
        output << "# ip\tbanUntilEpochMs\tbanLevel\triskScore\toffenses\tlastReason\n";
        for (const auto& [ip, state] : ip_states_) {
            if (state.banned_until_epoch_ms == 0 && state.risk_score == 0 &&
                state.offenses == 0) continue;
            std::string reason = state.last_reason;
            std::replace(reason.begin(), reason.end(), '\t', ' ');
            output << ip_string(ip) << '\t' << state.banned_until_epoch_ms << '\t'
                   << state.ban_level << '\t' << state.risk_score << '\t'
                   << state.offenses << '\t' << reason << '\n';
        }
        output.close();
        std::error_code error;
        fs::rename(temporary, firewall_bans_path_, error);
        if (error) {
            fs::remove(firewall_bans_path_, error);
            error.clear();
            fs::rename(temporary, firewall_bans_path_, error);
        }
    }

    bool is_allowlisted(const IpKey& ip) const {
        return allowlisted_ips_.contains(ip);
    }

    bool is_denylisted(const IpKey& ip) const {
        return denylisted_ips_.contains(ip);
    }

    void decay_risk(IpState& state, Clock::time_point now) {
        if (state.risk_updated.time_since_epoch().count() == 0) {
            state.risk_updated = now;
            return;
        }
        if (config_.risk_decay_per_minute <= 0 || state.risk_score <= 0) {
            state.risk_updated = now;
            return;
        }
        const auto minutes = std::chrono::duration_cast<std::chrono::minutes>(
            now - state.risk_updated).count();
        if (minutes <= 0) return;
        state.risk_score = std::max(0, state.risk_score -
            static_cast<int>(minutes) * config_.risk_decay_per_minute);
        state.risk_updated += std::chrono::minutes(minutes);
    }

    void apply_progressive_ban(IpState& state, const IpKey& ip,
                               const std::string& reason) {
        const auto index = std::min<std::size_t>(
            static_cast<std::size_t>(std::max(0, state.ban_level)),
            config_.progressive_ban_seconds.size() - 1);
        const int seconds = std::max(1, config_.progressive_ban_seconds[index]);
        state.ban_level = std::min<int>(state.ban_level + 1,
                                       static_cast<int>(config_.progressive_ban_seconds.size() - 1));
        state.banned_until = Clock::now() + std::chrono::seconds(seconds);
        state.banned_until_epoch_ms = epoch_ms() + static_cast<std::uint64_t>(seconds) * 1000ULL;
        state.risk_score = std::max(0, config_.risk_warning_threshold / 2);
        state.last_reason = reason;
        counters_.temporary_bans++;
        save_persistent_bans();
        log_event("firewall.progressive_ban",
                  "Progressive ban " + ip_string(ip) + " for " +
                      std::to_string(seconds) + "s: " + reason,
                  "warning", ip_string(ip));
    }

    void add_risk(IpState& state, const IpKey& ip, int points,
                  const std::string& reason) {
        if (!config_.adaptive_firewall_enabled || points <= 0 || is_allowlisted(ip)) return;
        const auto now = Clock::now();
        decay_risk(state, now);
        const int before = state.risk_score;
        state.risk_score = std::min(10000, state.risk_score + points);
        state.risk_updated = now;
        state.offenses++;
        state.last_reason = reason;
        if (before < config_.risk_warning_threshold &&
            state.risk_score >= config_.risk_warning_threshold) {
            counters_.adaptive_warnings++;
            log_event("firewall.risk_warning",
                      "Risk warning " + ip_string(ip) + " score=" +
                          std::to_string(state.risk_score) + ": " + reason,
                      "warning", ip_string(ip));
        }
        if (state.risk_score >= config_.risk_ban_threshold) {
            apply_progressive_ban(state, ip, reason);
        }
    }

    void apply_live_values(const std::map<std::string, std::string>& values) {
        Config next = config_;
        const auto set_unsigned = [&](const char* key, std::size_t& target) {
            if (const auto it = values.find(key); it != values.end()) {
                target = parse_unsigned(it->second, target);
            }
        };
        const auto set_int = [&](const char* key, int& target) {
            if (const auto it = values.find(key); it != values.end()) {
                target = static_cast<int>(parse_unsigned(it->second, target));
            }
        };
        const auto set_bool = [&](const char* key, bool& target) {
            if (const auto it = values.find(key); it != values.end()) {
                target = parse_bool(it->second, target);
            }
        };
        if (const auto it = values.find("routing_mode"); it != values.end()) {
            const auto mode = trim(it->second);
            if (mode != "primary" && mode != "failover" && mode != "round_robin" &&
                mode != "least_sessions") {
                throw std::runtime_error("Invalid live routing_mode");
            }
            next.routing_mode = mode;
        }
        set_bool("firewall_enabled", next.firewall_enabled);
        set_bool("adaptive_firewall_enabled", next.adaptive_firewall_enabled);
        set_unsigned("max_datagram_size", next.max_datagram_size);
        set_unsigned("max_packets_per_second_per_ip", next.max_packets_per_second_per_ip);
        set_unsigned("global_packets_per_second", next.global_packets_per_second);
        set_unsigned("max_handshakes_per_minute", next.max_handshakes_per_minute);
        set_unsigned("ping_limit_per_second_per_ip", next.ping_limit_per_second_per_ip);
        set_int("risk_decay_per_minute", next.risk_decay_per_minute);
        set_int("risk_warning_threshold", next.risk_warning_threshold);
        set_int("risk_ban_threshold", next.risk_ban_threshold);
        if (const auto it = values.find("progressive_ban_seconds"); it != values.end()) {
            next.progressive_ban_seconds = parse_integer_list(it->second, next.progressive_ban_seconds);
        }
        set_int("health_failure_threshold", next.health_failure_threshold);
        set_int("health_recovery_threshold", next.health_recovery_threshold);
        set_bool("incident_mode_enabled", next.incident_mode_enabled);
        set_unsigned("incident_trigger_packets_per_second",
                     next.incident_trigger_packets_per_second);
        set_unsigned("incident_min_packets_per_second",
                     next.incident_min_packets_per_second);
        set_int("incident_recovery_seconds", next.incident_recovery_seconds);
        set_int("incident_rate_divisor", next.incident_rate_divisor);
        set_int("incident_handshake_divisor", next.incident_handshake_divisor);
        if (const auto found = values.find("incident_trigger_drop_ratio");
            found != values.end()) {
            try {
                next.incident_trigger_drop_ratio =
                    std::clamp(std::stod(found->second), 0.01, 1.0);
            } catch (...) {}
        }
        set_bool("packet_capture_enabled", next.packet_capture_enabled);
        set_bool("capture_outgoing", next.capture_outgoing);
        set_unsigned("packet_hex_preview_bytes", next.packet_hex_preview_bytes);
        set_int("stats_interval_seconds", next.stats_interval_seconds);
        set_int("state_interval_ms", next.state_interval_ms);
        next.risk_warning_threshold = std::max(1, next.risk_warning_threshold);
        next.risk_ban_threshold = std::max(next.risk_warning_threshold + 1,
                                           next.risk_ban_threshold);
        if (next.progressive_ban_seconds.empty()) {
            throw std::runtime_error("progressive_ban_seconds cannot be empty");
        }
        config_ = std::move(next);
    }

    bool reload_live_config(bool force) {
        std::error_code error;
        const auto modified = fs::last_write_time(live_config_path_, error);
        if (!force && !error && live_config_mtime_ == modified) return false;
        try {
            const auto values = load_properties(live_config_path_);
            apply_live_values(values);
            load_firewall_files();
            load_protection_profiles();
            if (!error) live_config_mtime_ = modified;
            config_version_++;
            last_config_reload_epoch_ms_ = epoch_ms();
            last_config_error_.clear();
            counters_.config_reloads++;
            log_event("config.reloaded",
                      "Live edge policy reloaded, version " +
                          std::to_string(config_version_), "info");
            return true;
        } catch (const std::exception& exception) {
            last_config_error_ = exception.what();
            counters_.config_reload_failures++;
            log_event("config.reload_failed",
                      "Live policy reload failed: " + last_config_error_, "error");
            return false;
        }
    }

    void handle_live_config(Clock::time_point now) {
        if (now - last_live_config_check_ <
            std::chrono::milliseconds(config_.live_config_reload_ms)) return;
        last_live_config_check_ = now;
        reload_live_config(false);
    }

    void initialize_backends() {
        std::uint64_t seed = std::random_device{}();
        random_.seed(seed);
        for (const auto& backend_config : config_.backends) {
            Backend backend{};
            backend.config = backend_config;
            backend.manual_enabled = backend_config.enabled;
            backend.endpoint = resolve_udp(backend_config.host, backend_config.port);
            backend.health_fd = create_connected_udp(backend.endpoint, 1024 * 1024, 1024 * 1024);
            if (backend.health_fd < 0) {
                throw std::runtime_error("Unable to create health socket for " + backend_config.name);
            }
            backend.last_ping_attempt = Clock::now() -
                                        std::chrono::seconds(config_.health_interval_seconds);
            backend.last_response = Clock::now();
            backends_.push_back(std::move(backend));
        }

        const auto primary = std::find_if(backends_.begin(), backends_.end(), [&](const Backend& backend) {
            return backend.config.name == config_.primary_backend;
        });
        if (primary == backends_.end()) {
            config_.primary_backend = backends_.front().config.name;
        }

        for (const auto& route : config_.static_routes) {
            if (!backend_index_by_name(route.backend_name)) {
                throw std::runtime_error(
                    "Static route :" + std::to_string(route.listener_port) +
                    " references unknown backend '" + route.backend_name + "'.");
            }
        }
    }

    void add_epoll(int fd) {
        epoll_event event{};
        event.events = EPOLLIN | EPOLLERR | EPOLLHUP;
        event.data.fd = fd;
        if (epoll_ctl(epoll_fd_, EPOLL_CTL_ADD, fd, &event) != 0) {
            throw std::runtime_error("epoll_ctl add: " + std::string(std::strerror(errno)));
        }
    }

    void add_listener(std::uint16_t port) {
        if (listener_port_to_fd_.contains(port)) {
            return;
        }
        const int fd = create_listener(port, config_.socket_receive_buffer,
                                       config_.socket_send_buffer);
        add_epoll(fd);
        listener_fd_to_port_[fd] = port;
        listener_port_to_fd_[port] = fd;
        if (port == config_.listen_port) {
            listener_fd_ = fd;
        }
    }

    void handle_listener(int listener_fd, std::uint16_t listener_port, Clock::time_point now) {
        constexpr unsigned kBatch = 32;
        std::array<std::array<std::byte, 65536>, kBatch> buffers{};
        std::array<sockaddr_storage, kBatch> addresses{};
        std::array<iovec, kBatch> iovecs{};
        std::array<mmsghdr, kBatch> messages{};

        for (;;) {
            for (unsigned index = 0; index < kBatch; ++index) {
                iovecs[index].iov_base = buffers[index].data();
                iovecs[index].iov_len = buffers[index].size();
                messages[index] = {};
                messages[index].msg_hdr.msg_name = &addresses[index];
                messages[index].msg_hdr.msg_namelen = sizeof(sockaddr_storage);
                messages[index].msg_hdr.msg_iov = &iovecs[index];
                messages[index].msg_hdr.msg_iovlen = 1;
            }

            const int received = recvmmsg(listener_fd, messages.data(), kBatch, MSG_DONTWAIT, nullptr);
            if (received < 0) {
                if (errno == EAGAIN || errno == EWOULDBLOCK) {
                    return;
                }
                if (errno == EINTR) {
                    continue;
                }
                std::cerr << "[Ninj-OS Edge] recvmmsg failed: " << std::strerror(errno) << "\n";
                return;
            }
            if (received == 0) {
                return;
            }

            for (int index = 0; index < received; ++index) {
                process_client_datagram(listener_fd, listener_port, addresses[index],
                                        messages[index].msg_hdr.msg_namelen, buffers[index].data(),
                                        messages[index].msg_len, now);
            }
            if (received < static_cast<int>(kBatch)) {
                return;
            }
        }
    }

    const ProtectionProfile& profile_named(const std::string& name) const {
        const auto found = protection_profiles_.find(name);
        if (found != protection_profiles_.end()) {
            return found->second;
        }
        return protection_profiles_.at("default");
    }

    const ProtectionProfile& profile_for_listener(
        std::uint16_t listener_port,
        const EndpointKey& endpoint) const {
        const auto existing = by_client_.find(endpoint);
        if (existing != by_client_.end()) {
            const auto session = sessions_.find(existing->second);
            if (session != sessions_.end() && session->second.backend_index < backends_.size()) {
                return profile_named(
                    backends_[session->second.backend_index].config.protection_profile);
            }
        }

        const auto route = std::find_if(
            config_.static_routes.begin(), config_.static_routes.end(),
            [&](const StaticRouteConfig& item) {
                return item.listener_port == listener_port;
            });
        if (route != config_.static_routes.end()) {
            const auto index = backend_index_by_name(route->backend_name);
            if (index) {
                return profile_named(backends_[*index].config.protection_profile);
            }
        }
        return profile_named("default");
    }

    void process_client_datagram(int listener_fd,
                                 std::uint16_t listener_port,
                                 const sockaddr_storage& client,
                                 socklen_t client_length,
                                 const std::byte* data,
                                 std::size_t size,
                                 Clock::time_point now) {
        counters_.client_packets++;
        counters_.client_bytes += size;
        global_packets_in_window_++;

        const unsigned char id = size > 0 ? std::to_integer<unsigned char>(data[0]) : 0xFF;
        const std::string type = size > 0 ? raknet_name(id) : "EmptyDatagram";
        const EndpointKey endpoint = EndpointKey::from(client, listener_port);
        const IpKey ip = IpKey::from(client);
        const ProtectionProfile& protection = profile_for_listener(listener_port, endpoint);
        auto& ip_state = ip_states_[ip];
        initialize_ip_windows(ip_state, now);

        std::string action = "forward";
        std::string backend_name;

        if (size == 0) {
            action = "drop-empty";
            drop(ip_state, ip, "empty datagram", false);
            log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
            return;
        }

        if (is_denylisted(ip)) {
            action = "drop-denylist";
            counters_.dropped_packets++;
            counters_.denylist_drops++;
            ip_state.total_dropped++;
            log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
            return;
        }

        decay_risk(ip_state, now);
        if (is_banned(ip_state, now) && !is_allowlisted(ip)) {
            action = "drop-banned";
            counters_.dropped_packets++;
            ip_state.total_dropped++;
            log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
            return;
        }

        if (config_.firewall_enabled && !is_allowlisted(ip)) {
            if (size > protection.max_datagram_size) {
                action = "drop-oversized";
                drop(ip_state, ip, "oversized datagram", true, 25);
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }

            if (now - global_window_ >= std::chrono::seconds(1)) {
                global_window_ = now;
                global_packets_in_window_ = 1;
            }
            if (global_packets_in_window_ > config_.global_packets_per_second) {
                action = "drop-global-rate";
                counters_.dropped_packets++;
                counters_.rate_limited++;
                ip_state.total_dropped++;
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }

            if (now - ip_state.packet_window >= std::chrono::seconds(1)) {
                ip_state.packet_window = now;
                ip_state.packets_in_window = 0;
            }
            const auto packet_limit = incident_mode_
                ? std::max<std::size_t>(100,
                    protection.max_packets_per_second_per_ip /
                    static_cast<std::size_t>(config_.incident_rate_divisor))
                : protection.max_packets_per_second_per_ip;
            if (++ip_state.packets_in_window > packet_limit) {
                action = "drop-ip-rate";
                drop(ip_state, ip, "packet rate exceeded", true, 15);
                counters_.rate_limited++;
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }

            if (is_handshake_packet(id)) {
                if (now - ip_state.handshake_window >= std::chrono::minutes(1)) {
                    ip_state.handshake_window = now;
                    ip_state.handshakes_in_window = 0;
                }
                const auto handshake_limit = incident_mode_
                    ? std::max<std::size_t>(1,
                        protection.max_handshakes_per_minute /
                        static_cast<std::size_t>(config_.incident_handshake_divisor))
                    : protection.max_handshakes_per_minute;
                if (++ip_state.handshakes_in_window > handshake_limit) {
                    action = "drop-handshake-rate";
                    drop(ip_state, ip, "handshake rate exceeded", true, 20);
                    counters_.rate_limited++;
                    log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                    return;
                }
            }
        }

        if (is_unconnected_ping(id) && config_.ping_cache_enabled) {
            if (now - ip_state.ping_window >= std::chrono::seconds(1)) {
                ip_state.ping_window = now;
                ip_state.pings_in_window = 0;
            }
            if (++ip_state.pings_in_window > config_.ping_limit_per_second_per_ip) {
                action = "drop-ping-rate";
                drop(ip_state, ip, "ping rate exceeded", true, 5);
                counters_.rate_limited++;
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }

            std::optional<std::size_t> selected;
            if (has_static_route(listener_port)) {
                selected = static_backend_for_listener(listener_port);
            } else if (listener_port == config_.listen_port) {
                selected = choose_backend(false);
            } else {
                selected = transfer_backend_for_ping(listener_port, ip, now);
            }
            if (selected && !backends_[*selected].cached_pong.empty()) {
                Backend& backend = backends_[*selected];
                backend_name = backend.config.name;
                std::string motd;
                if (maintenance_) {
                    motd = config_.maintenance_motd;
                } else if (drain_) {
                    motd = config_.drain_motd;
                }
                const auto pong = customize_pong(backend.cached_pong, data, size, motd);
                const ssize_t sent = sendto(listener_fd, pong.data(), pong.size(), MSG_NOSIGNAL,
                                            reinterpret_cast<const sockaddr*>(&client), client_length);
                if (sent >= 0) {
                    counters_.cached_ping_replies++;
                    counters_.server_packets++;
                    counters_.server_bytes += static_cast<std::uint64_t>(sent);
                    action = "cached-pong";
                    log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                    if (config_.capture_outgoing) {
                        log_packet("gateway_to_client", client, backend_name, "UnconnectedPong", 0x1C,
                                   pong.size(), "send", pong.data());
                    }
                    return;
                }
            }
        }

        auto existing = by_client_.find(endpoint);
        int upstream_fd = -1;
        if (existing == by_client_.end()) {
            if (maintenance_ || drain_ ||
                (incident_mode_ && !protection.allow_new_sessions_during_incident)) {
                action = maintenance_ ? "drop-maintenance"
                         : drain_ ? "drop-drain"
                         : "drop-incident-new-session";
                counters_.dropped_packets++;
                ip_state.total_dropped++;
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }
            if (sessions_.size() >= config_.max_sessions ||
                ip_state.active_sessions >= protection.max_sessions_per_ip) {
                action = "drop-session-limit";
                drop(ip_state, ip, "session limit reached", true, 10);
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }

            std::optional<std::size_t> backend_index;
            std::string transfer_ticket_id;
            const bool static_route = has_static_route(listener_port);

            if (static_route) {
                backend_index = static_backend_for_listener(listener_port);
            } else if (listener_port == config_.listen_port) {
                backend_index = choose_backend(true);
            } else {
                const auto route = consume_transfer_ticket(listener_port, ip, now);
                if (route) {
                    backend_index = route->backend_index;
                    transfer_ticket_id = route->ticket_id;
                }
            }
            if (!backend_index) {
                if (static_route) {
                    action = "drop-static-route-unavailable";
                } else if (listener_port == config_.listen_port) {
                    action = "drop-no-backend";
                } else {
                    action = "drop-transfer-ticket";
                }
                counters_.dropped_packets++;
                if (!static_route && listener_port != config_.listen_port) {
                    counters_.transfer_ticket_rejections++;
                }
                ip_state.total_dropped++;
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }
            Backend& backend = backends_[*backend_index];
            backend_name = backend.config.name;
            upstream_fd = create_connected_udp(backend.endpoint, config_.socket_receive_buffer,
                                               config_.socket_send_buffer);
            if (upstream_fd < 0) {
                action = "drop-upstream-socket";
                counters_.dropped_packets++;
                ip_state.total_dropped++;
                log_packet("client_to_gateway", client, backend_name, type, id, size, action, data);
                return;
            }
            try {
                add_epoll(upstream_fd);
            } catch (...) {
                close(upstream_fd);
                throw;
            }

            sockaddr_storage upstream_local{};
            socklen_t upstream_local_length = sizeof(upstream_local);
            std::uint16_t upstream_local_port = 0;
            if (getsockname(upstream_fd, reinterpret_cast<sockaddr*>(&upstream_local),
                            &upstream_local_length) == 0) {
                upstream_local_port = sockaddr_port(upstream_local);
            }

            Session session{};
            session.fd = upstream_fd;
            session.listener_fd = listener_fd;
            session.listener_port = listener_port;
            session.upstream_local_port = upstream_local_port;
            session.transfer_ticket_id = transfer_ticket_id;
            session.client = client;
            session.client_length = client_length;
            session.ip = ip;
            session.backend_index = *backend_index;
            session.created = now;
            session.last_activity = now;
            sessions_.emplace(upstream_fd, session);
            by_client_.emplace(endpoint, upstream_fd);
            ip_state.active_sessions++;
            backend.active_sessions++;
            counters_.sessions_opened++;
            log_event("session.opened",
                      "Session opened " + endpoint_string(client) + " via :" +
                          std::to_string(listener_port) + " -> " + backend.config.name +
                          (transfer_ticket_id.empty() ? "" : " ticket=" + transfer_ticket_id),
                      "info", ip_string(ip), backend.config.name);
        } else {
            upstream_fd = existing->second;
            const auto session = sessions_.find(upstream_fd);
            if (session == sessions_.end()) {
                by_client_.erase(existing);
                counters_.dropped_packets++;
                return;
            }
            backend_name = backends_[session->second.backend_index].config.name;
        }

        auto session = sessions_.find(upstream_fd);
        if (session == sessions_.end()) {
            return;
        }
        const ssize_t sent = send(upstream_fd, data, size, MSG_NOSIGNAL);
        if (sent < 0) {
            action = "drop-upstream-send";
            counters_.dropped_packets++;
            ip_state.total_dropped++;
        } else {
            session->second.last_activity = now;
            session->second.client_to_server_bytes += static_cast<std::uint64_t>(sent);
            session->second.client_packets++;
        }
        log_packet("client_to_server", client, backend_name, type, id, size, action, data);
    }

    void handle_backend_data(int fd, Clock::time_point now) {
        auto session_it = sessions_.find(fd);
        if (session_it == sessions_.end()) {
            return;
        }
        Session& session = session_it->second;
        Backend& backend = backends_[session.backend_index];
        std::array<std::byte, 65536> buffer{};

        for (;;) {
            const ssize_t count = recv(fd, buffer.data(), buffer.size(), MSG_DONTWAIT);
            if (count < 0) {
                if (errno == EAGAIN || errno == EWOULDBLOCK) {
                    return;
                }
                if (errno == EINTR) {
                    continue;
                }
                close_session(fd, "backend-receive-error");
                return;
            }
            if (count == 0) {
                return;
            }

            const ssize_t sent = sendto(session.listener_fd, buffer.data(), static_cast<std::size_t>(count),
                                        MSG_NOSIGNAL,
                                        reinterpret_cast<const sockaddr*>(&session.client),
                                        session.client_length);
            if (sent < 0) {
                continue;
            }
            session.last_activity = now;
            session.server_to_client_bytes += static_cast<std::uint64_t>(sent);
            session.server_packets++;
            counters_.server_packets++;
            counters_.server_bytes += static_cast<std::uint64_t>(sent);

            if (config_.capture_outgoing) {
                const unsigned char id = count > 0
                                             ? std::to_integer<unsigned char>(buffer[0])
                                             : 0xFF;
                log_packet("server_to_client", session.client, backend.config.name,
                           count > 0 ? raknet_name(id) : "EmptyDatagram", id,
                           static_cast<std::size_t>(count), "forward", buffer.data());
            }
        }
    }

    void handle_incident_mode(Clock::time_point now) {
        if (!config_.incident_mode_enabled ||
            now - last_incident_check_ < std::chrono::seconds(1)) {
            return;
        }

        const auto elapsed = std::chrono::duration<double>(now - last_incident_check_).count();
        if (elapsed <= 0.0) {
            return;
        }

        const auto packet_delta = counters_.client_packets - incident_last_client_packets_;
        const auto drop_delta = counters_.dropped_packets - incident_last_dropped_packets_;
        incident_last_client_packets_ = counters_.client_packets;
        incident_last_dropped_packets_ = counters_.dropped_packets;
        last_incident_check_ = now;

        const auto packets_per_second =
            static_cast<std::size_t>(static_cast<double>(packet_delta) / elapsed);
        const double drop_ratio =
            packet_delta == 0 ? 0.0
                              : static_cast<double>(drop_delta) /
                                    static_cast<double>(packet_delta);

        incident_last_pps_ = packets_per_second;
        incident_last_drop_ratio_ = drop_ratio;

        const bool should_enter =
            packets_per_second >= config_.incident_trigger_packets_per_second ||
            (packets_per_second >= config_.incident_min_packets_per_second &&
             drop_ratio >= config_.incident_trigger_drop_ratio);

        if (!incident_mode_ && should_enter) {
            incident_mode_ = true;
            incident_last_change_epoch_ms_ = epoch_ms();
            incident_recovery_candidate_ = {};
            counters_.incident_entries++;
            log_event("incident.entered",
                      "Automatic incident mode entered pps=" +
                          std::to_string(packets_per_second) +
                          " dropRatio=" + std::to_string(drop_ratio),
                      "warning");
            return;
        }

        if (!incident_mode_) {
            return;
        }

        if (should_enter) {
            incident_recovery_candidate_ = {};
            return;
        }

        if (incident_recovery_candidate_.time_since_epoch().count() == 0) {
            incident_recovery_candidate_ = now;
            return;
        }

        if (now - incident_recovery_candidate_ >=
            std::chrono::seconds(config_.incident_recovery_seconds)) {
            incident_mode_ = false;
            incident_last_change_epoch_ms_ = epoch_ms();
            incident_recovery_candidate_ = {};
            counters_.incident_exits++;
            log_event("incident.recovered",
                      "Automatic incident mode cleared after stable traffic",
                      "info");
        }
    }

    void handle_health_checks(Clock::time_point now) {
        if (!config_.health_enabled && !config_.ping_cache_enabled) {
            return;
        }

        for (std::size_t index = 0; index < backends_.size(); ++index) {
            Backend& backend = backends_[index];
            if (backend.awaiting_health_response &&
                now - backend.ping_sent >= std::chrono::milliseconds(config_.health_timeout_ms)) {
                backend.awaiting_health_response = false;
                backend.failure_count++;
                backend.recovery_count = 0;
                counters_.health_failures++;
                if (backend.failure_count >= config_.health_failure_threshold && backend.healthy) {
                    backend.healthy = false;
                    log_event("backend.down", "Backend marked unhealthy: " + backend.config.name,
                              "error", {}, backend.config.name);
                }
            }

            int check_interval = config_.health_enabled
                ? config_.health_interval_seconds
                : config_.ping_cache_refresh_seconds;
            if (config_.health_enabled && config_.ping_cache_enabled) {
                check_interval = std::min(config_.health_interval_seconds,
                                          config_.ping_cache_refresh_seconds);
            }
            if (now - backend.last_ping_attempt < std::chrono::seconds(check_interval)) {
                continue;
            }

            backend.last_ping_attempt = now;
            backend.ping_token = steady_ms() ^ random_();
            const auto packet = make_unconnected_ping(backend.ping_token, random_());
            const ssize_t sent = send(backend.health_fd, packet.data(), packet.size(), MSG_NOSIGNAL);
            if (sent >= 0) {
                backend.awaiting_health_response = true;
                backend.ping_sent = now;
                counters_.health_checks++;
            }
        }
    }

    void handle_health_response(std::size_t backend_index, Clock::time_point now) {
        Backend& backend = backends_[backend_index];
        std::array<std::byte, 65536> buffer{};
        for (;;) {
            const ssize_t count = recv(backend.health_fd, buffer.data(), buffer.size(), MSG_DONTWAIT);
            if (count < 0) {
                if (errno == EAGAIN || errno == EWOULDBLOCK) {
                    return;
                }
                if (errno == EINTR) {
                    continue;
                }
                return;
            }
            if (count < 9 || std::to_integer<unsigned char>(buffer[0]) != 0x1C) {
                continue;
            }

            const auto token = read_be64(buffer.data() + 1);
            if (backend.awaiting_health_response && token == backend.ping_token) {
                backend.latency_ms = static_cast<double>(
                    std::chrono::duration_cast<std::chrono::microseconds>(now - backend.ping_sent)
                        .count()) /
                                     1000.0;
                backend.awaiting_health_response = false;
                backend.failure_count = 0;
                backend.recovery_count++;
                backend.last_response = now;
                if (!backend.healthy &&
                    backend.recovery_count >= config_.health_recovery_threshold) {
                    backend.healthy = true;
                    log_event("backend.up", "Backend recovered: " + backend.config.name,
                              "info", {}, backend.config.name);
                }
            }
            backend.cached_pong.assign(buffer.begin(), buffer.begin() + count);
        }
    }

    std::optional<std::size_t> choose_backend(bool for_session) {
        std::vector<std::size_t> available;
        std::optional<std::size_t> primary;
        for (std::size_t index = 0; index < backends_.size(); ++index) {
            const Backend& backend = backends_[index];
            if (backend.config.name == config_.primary_backend) {
                primary = index;
            }
            if (!backend.manual_enabled) {
                continue;
            }
            if (config_.health_enabled && !backend.healthy) {
                continue;
            }
            available.push_back(index);
        }

        if (available.empty()) {
            return std::nullopt;
        }

        if (config_.routing_mode == "primary") {
            if (primary && std::find(available.begin(), available.end(), *primary) != available.end()) {
                return primary;
            }
            return std::nullopt;
        }

        if (config_.routing_mode == "failover") {
            if (primary && std::find(available.begin(), available.end(), *primary) != available.end()) {
                return primary;
            }
            const auto fallback = std::find_if(available.begin(), available.end(), [&](std::size_t index) {
                return backends_[index].config.fallback;
            });
            return fallback != available.end() ? std::optional<std::size_t>(*fallback)
                                               : std::optional<std::size_t>(available.front());
        }

        if (config_.routing_mode == "round_robin") {
            const auto selected = available[round_robin_index_++ % available.size()];
            return selected;
        }

        if (config_.routing_mode == "least_sessions") {
            return *std::min_element(available.begin(), available.end(), [&](std::size_t left,
                                                                            std::size_t right) {
                return backends_[left].active_sessions < backends_[right].active_sessions;
            });
        }

        if (for_session) {
            return primary && std::find(available.begin(), available.end(), *primary) != available.end()
                       ? primary
                       : std::optional<std::size_t>(available.front());
        }
        return available.front();
    }

    std::optional<std::size_t> static_backend_for_listener(
        std::uint16_t listener_port) const {
        const auto route = std::find_if(
            config_.static_routes.begin(), config_.static_routes.end(),
            [&](const StaticRouteConfig& item) {
                return item.listener_port == listener_port;
            });
        if (route == config_.static_routes.end()) {
            return std::nullopt;
        }

        const auto index = backend_index_by_name(route->backend_name);
        if (!index || !backend_available(*index)) {
            return std::nullopt;
        }
        return index;
    }

    bool has_static_route(std::uint16_t listener_port) const {
        return std::any_of(
            config_.static_routes.begin(), config_.static_routes.end(),
            [&](const StaticRouteConfig& item) {
                return item.listener_port == listener_port;
            });
    }

    std::optional<std::size_t> backend_index_by_name(const std::string& name) const {
        for (std::size_t index = 0; index < backends_.size(); ++index) {
            if (backends_[index].config.name == name) {
                return index;
            }
        }
        return std::nullopt;
    }

    bool backend_available(std::size_t index) const {
        if (index >= backends_.size()) {
            return false;
        }
        const auto& backend = backends_[index];
        return backend.manual_enabled && (!config_.health_enabled || backend.healthy);
    }

    void refresh_transfer_tickets(Clock::time_point now, bool force) {
        if (!config_.transfer_enabled) {
            transfer_tickets_.clear();
            return;
        }
        if (!force && now - last_ticket_reload_ <
                          std::chrono::milliseconds(config_.transfer_ticket_reload_ms)) {
            return;
        }
        last_ticket_reload_ = now;

        std::ifstream input(transfer_ticket_path_);
        if (!input) {
            return;
        }
        std::unordered_map<std::uint16_t, TransferTicket> loaded;
        std::string line;
        const auto current_epoch = epoch_ms();
        while (std::getline(input, line)) {
            if (line.empty() || line[0] == '#') {
                continue;
            }
            const auto fields = split(line, '\t');
            if (fields.size() < 9) {
                continue;
            }
            const auto port_value = parse_unsigned(fields[1], 0);
            const auto expires = parse_unsigned(fields[7], 0);
            if (port_value < config_.transfer_port_start ||
                port_value > config_.transfer_port_end || expires <= current_epoch ||
                has_static_route(static_cast<std::uint16_t>(port_value))) {
                continue;
            }
            const auto backend_index = backend_index_by_name(trim(fields[2]));
            if (!backend_index) {
                continue;
            }
            TransferTicket ticket{};
            ticket.id = trim(fields[0]);
            ticket.port = static_cast<std::uint16_t>(port_value);
            ticket.backend_index = *backend_index;
            ticket.backend_name = trim(fields[2]);
            ticket.source_ip = trim(fields[3]);
            ticket.xuid = trim(fields[4]);
            ticket.player = trim(fields[5]);
            ticket.source_server = trim(fields[6]);
            ticket.expires_at_ms = expires;
            ticket.consumed = consumed_transfer_ticket_ids_.contains(ticket.id);
            if (!ticket.id.empty()) {
                loaded[ticket.port] = std::move(ticket);
            }
        }
        std::unordered_set<std::string> active_ids;
        for (const auto& [port, ticket] : loaded) {
            (void)port;
            active_ids.insert(ticket.id);
        }
        for (auto iterator = consumed_transfer_ticket_ids_.begin();
             iterator != consumed_transfer_ticket_ids_.end();) {
            if (!active_ids.contains(*iterator)) {
                iterator = consumed_transfer_ticket_ids_.erase(iterator);
            } else {
                ++iterator;
            }
        }
        counters_.transfer_tickets_loaded += loaded.size();
        transfer_tickets_ = std::move(loaded);
    }

    bool ticket_matches_ip(const TransferTicket& ticket, const IpKey& ip) const {
        if (!config_.transfer_require_source_ip || ticket.source_ip.empty()) {
            return true;
        }
        return ticket.source_ip == ip_string(ip);
    }

    std::optional<std::size_t> transfer_backend_for_ping(std::uint16_t port,
                                                         const IpKey& ip,
                                                         Clock::time_point now) {
        refresh_transfer_tickets(now, false);
        const auto found = transfer_tickets_.find(port);
        if (found == transfer_tickets_.end() || found->second.consumed ||
            found->second.expires_at_ms <= epoch_ms() ||
            !ticket_matches_ip(found->second, ip) ||
            !backend_available(found->second.backend_index)) {
            return std::nullopt;
        }
        return found->second.backend_index;
    }

    std::optional<TicketRoute> consume_transfer_ticket(std::uint16_t port,
                                                        const IpKey& ip,
                                                        Clock::time_point now) {
        refresh_transfer_tickets(now, false);
        const auto found = transfer_tickets_.find(port);
        if (found == transfer_tickets_.end()) {
            return std::nullopt;
        }
        auto& ticket = found->second;
        if (ticket.consumed || ticket.expires_at_ms <= epoch_ms() ||
            !ticket_matches_ip(ticket, ip) || !backend_available(ticket.backend_index)) {
            return std::nullopt;
        }
        ticket.consumed = true;
        consumed_transfer_ticket_ids_.insert(ticket.id);
        counters_.transfer_tickets_consumed++;
        log_event("transfer.ticket_consumed",
                  "Transfer ticket consumed ticket=" + ticket.id +
                      " player=" + ticket.player + " xuid=" + ticket.xuid +
                      " source=" + ticket.source_server + " destination=" + ticket.backend_name +
                      " port=" + std::to_string(ticket.port),
                  "info", ip_string(ip), ticket.backend_name);
        return TicketRoute{ticket.backend_index, ticket.id};
    }

    std::size_t active_transfer_ticket_count() const {
        const auto current = epoch_ms();
        return static_cast<std::size_t>(std::count_if(
            transfer_tickets_.begin(), transfer_tickets_.end(),
            [&](const auto& entry) {
                return !entry.second.consumed && entry.second.expires_at_ms > current;
            }));
    }

    void initialize_ip_windows(IpState& state, Clock::time_point now) {
        if (state.packet_window.time_since_epoch().count() == 0) {
            state.packet_window = now;
            state.ping_window = now;
            state.handshake_window = now;
        }
    }

    bool is_banned(const IpState& state, Clock::time_point now) const {
        return (state.banned_until.time_since_epoch().count() != 0 && now < state.banned_until) ||
               (state.banned_until_epoch_ms != 0 && epoch_ms() < state.banned_until_epoch_ms);
    }

    void drop(IpState& state, const IpKey& ip, const std::string& reason,
              bool strike, int risk_points = 10) {
        counters_.dropped_packets++;
        state.total_dropped++;
        if (!strike) {
            return;
        }
        state.strikes++;
        add_risk(state, ip, risk_points, reason);
        if (config_.adaptive_firewall_enabled) return;
        if (state.strikes < config_.strike_limit) return;
        state.strikes = 0;
        state.banned_until = Clock::now() + std::chrono::seconds(config_.temp_ban_seconds);
        state.banned_until_epoch_ms = epoch_ms() +
            static_cast<std::uint64_t>(config_.temp_ban_seconds) * 1000ULL;
        counters_.temporary_bans++;
        save_persistent_bans();
        log_event("firewall.temp_ban",
                  "Temporary ban for " + ip_string(ip) + ": " + reason,
                  "warning", ip_string(ip));
    }

    void close_session(int fd, const std::string& reason) {
        auto found = sessions_.find(fd);
        if (found == sessions_.end()) {
            return;
        }
        const Session session = found->second;
        const EndpointKey endpoint = EndpointKey::from(session.client, session.listener_port);
        auto ip_state = ip_states_.find(session.ip);
        if (ip_state != ip_states_.end() && ip_state->second.active_sessions > 0) {
            ip_state->second.active_sessions--;
        }
        if (session.backend_index < backends_.size() &&
            backends_[session.backend_index].active_sessions > 0) {
            backends_[session.backend_index].active_sessions--;
        }
        epoll_ctl(epoll_fd_, EPOLL_CTL_DEL, fd, nullptr);
        close(fd);
        by_client_.erase(endpoint);
        sessions_.erase(found);
        counters_.sessions_closed++;
        log_event("session.closed",
                  "Session closed " + endpoint_string(session.client) + " reason=" + reason,
                  "info", ip_string(session.ip),
                  session.backend_index < backends_.size()
                      ? backends_[session.backend_index].config.name
                      : std::string{});
    }

    void cleanup(Clock::time_point now) {
        if (now - last_cleanup_ < std::chrono::seconds(config_.cleanup_interval_seconds)) {
            return;
        }
        last_cleanup_ = now;

        std::vector<int> expired;
        expired.reserve(sessions_.size());
        for (const auto& [fd, session] : sessions_) {
            const int timeout = session.client_packets <= 2 ? config_.handshake_timeout_seconds
                                                            : config_.idle_timeout_seconds;
            if (now - session.last_activity >= std::chrono::seconds(timeout)) {
                expired.push_back(fd);
            }
        }
        for (int fd : expired) {
            close_session(fd, "timeout");
        }

        const auto current_epoch = epoch_ms();
        for (auto iterator = transfer_tickets_.begin(); iterator != transfer_tickets_.end();) {
            if (iterator->second.expires_at_ms <= current_epoch) {
                iterator = transfer_tickets_.erase(iterator);
            } else {
                ++iterator;
            }
        }

        for (auto iterator = ip_states_.begin(); iterator != ip_states_.end();) {
            decay_risk(iterator->second, now);
            const bool ban_expired = !is_banned(iterator->second, now);
            if (ban_expired) {
                iterator->second.banned_until = {};
                iterator->second.banned_until_epoch_ms = 0;
            }
            const bool stale = now - iterator->second.packet_window > std::chrono::minutes(15);
            if (iterator->second.active_sessions == 0 && ban_expired && stale) {
                iterator = ip_states_.erase(iterator);
            } else {
                ++iterator;
            }
        }
    }

    void load_command_offset() {
        std::error_code error;
        command_offset_ = fs::file_size(command_path_, error);
        if (error) {
            command_offset_ = 0;
        }
    }

    void handle_commands(Clock::time_point now) {
        if (now - last_command_poll_ < std::chrono::milliseconds(config_.command_poll_ms)) {
            return;
        }
        last_command_poll_ = now;

        std::ifstream input(command_path_);
        if (!input) {
            return;
        }
        input.seekg(static_cast<std::streamoff>(command_offset_));
        std::string line;
        while (std::getline(input, line)) {
            command_offset_ = static_cast<std::uint64_t>(input.tellg());
            if (static_cast<std::streamoff>(command_offset_) < 0) {
                std::error_code error;
                command_offset_ = fs::file_size(command_path_, error);
            }
            apply_command(line);
        }
    }

    void apply_command(const std::string& line) {
        const auto fields = split(line, '|');
        if (fields.size() < 2) {
            return;
        }
        const std::string command = trim(fields[1]);
        const std::string argument = fields.size() >= 3 ? trim(fields[2]) : std::string{};
        const std::string argument2 = fields.size() >= 4 ? trim(fields[3]) : std::string{};

        if (command == "maintenance") {
            maintenance_ = parse_bool(argument, maintenance_);
            log_event("control.maintenance",
                      std::string("Maintenance ") + (maintenance_ ? "enabled" : "disabled"),
                      maintenance_ ? "warning" : "info");
        } else if (command == "drain") {
            drain_ = parse_bool(argument, drain_);
            log_event("control.drain", std::string("Drain mode ") + (drain_ ? "enabled" : "disabled"),
                      drain_ ? "warning" : "info");
        } else if (command == "ban") {
            manual_ban(argument, static_cast<int>(parse_unsigned(argument2, config_.temp_ban_seconds)));
        } else if (command == "unban") {
            manual_unban(argument);
        } else if (command == "backend") {
            const auto target = std::find_if(backends_.begin(), backends_.end(), [&](const Backend& backend) {
                return backend.config.name == argument;
            });
            if (target != backends_.end()) {
                target->manual_enabled = parse_bool(argument2, target->manual_enabled);
                log_event("control.backend",
                          "Backend " + argument + (target->manual_enabled ? " enabled" : " disabled"),
                          "warning", {}, argument);
            }
        } else if (command == "routing") {
            if (argument == "primary" || argument == "failover" || argument == "round_robin" ||
                argument == "least_sessions") {
                config_.routing_mode = argument;
                log_event("control.routing", "Routing mode changed to " + argument, "info");
            }
        } else if (command == "reload") {
            reload_live_config(true);
        } else if (command == "topology_restart") {
            restart_requested_ = true;
            restart_exit_code_ = 75;
            log_event("topology.restart_queued",
                      "Backend registry changed; restarting only the gateway", "warning");
            g_running.store(false);
        } else if (command == "service_restart") {
            restart_requested_ = true;
            restart_exit_code_ = 76;
            log_event("service.restart_queued",
                      "Dashboard-affecting configuration changed; restarting services", "warning");
            g_running.store(false);
        } else if (command == "risk_reset") {
            const auto key = parse_ip_key(argument);
            if (key) {
                auto found = ip_states_.find(*key);
                if (found != ip_states_.end()) {
                    found->second.risk_score = 0;
                    found->second.strikes = 0;
                    found->second.offenses = 0;
                    found->second.ban_level = 0;
                    found->second.banned_until = {};
                    found->second.banned_until_epoch_ms = 0;
                    save_persistent_bans();
                }
                log_event("firewall.risk_reset", "Risk reset for " + argument,
                          "info", argument);
            }
        }
    }

    std::optional<IpKey> parse_ip_key(const std::string& text) {
        IpKey key{};
        if (inet_pton(AF_INET, text.c_str(), key.address.data()) == 1) {
            key.family = AF_INET;
            return key;
        }
        if (inet_pton(AF_INET6, text.c_str(), key.address.data()) == 1) {
            key.family = AF_INET6;
            return key;
        }
        return std::nullopt;
    }

    void manual_ban(const std::string& ip_text, int seconds) {
        const auto key = parse_ip_key(ip_text);
        if (!key) {
            return;
        }
        auto& state = ip_states_[*key];
        initialize_ip_windows(state, Clock::now());
        const int duration = std::max(seconds, 1);
        state.banned_until = Clock::now() + std::chrono::seconds(duration);
        state.banned_until_epoch_ms = epoch_ms() + static_cast<std::uint64_t>(duration) * 1000ULL;
        state.last_reason = "manual";
        save_persistent_bans();
        log_event("firewall.manual_ban", "Manual ban for " + ip_text, "warning", ip_text);
    }

    void manual_unban(const std::string& ip_text) {
        const auto key = parse_ip_key(ip_text);
        if (!key) {
            return;
        }
        auto found = ip_states_.find(*key);
        if (found != ip_states_.end()) {
            found->second.banned_until = {};
            found->second.banned_until_epoch_ms = 0;
            found->second.strikes = 0;
        }
        save_persistent_bans();
        log_event("firewall.unban", "Unbanned " + ip_text, "info", ip_text);
    }

    void log_packet(const std::string& direction,
                    const sockaddr_storage& client,
                    const std::string& backend,
                    const std::string& type,
                    unsigned char id,
                    std::size_t size,
                    const std::string& action,
                    const std::byte* data) {
        if (!config_.packet_capture_enabled) {
            return;
        }
        std::ostringstream line;
        line << "{\"timestamp\":" << epoch_ms()
             << ",\"layer\":\"transport\""
             << ",\"direction\":\"" << json_escape(direction) << "\""
             << ",\"client\":\"" << json_escape(endpoint_string(client)) << "\""
             << ",\"ip\":\"" << json_escape(sockaddr_host(client)) << "\""
             << ",\"backend\":\"" << json_escape(backend) << "\""
             << ",\"packetId\":" << static_cast<unsigned>(id)
             << ",\"packetName\":\"" << json_escape(type) << "\""
             << ",\"size\":" << size
             << ",\"action\":\"" << json_escape(action) << "\""
             << ",\"hex\":\""
             << json_escape(hex_preview(data, size, config_.packet_hex_preview_bytes)) << "\"}"
             ;
        packet_log_.write(line.str());
    }

    void log_event(const std::string& type,
                   const std::string& message,
                   const std::string& severity,
                   const std::string& ip = {},
                   const std::string& backend = {}) {
        std::ostringstream line;
        line << "{\"timestamp\":" << epoch_ms()
             << ",\"type\":\"" << json_escape(type) << "\""
             << ",\"severity\":\"" << json_escape(severity) << "\""
             << ",\"message\":\"" << json_escape(message) << "\""
             << ",\"ip\":\"" << json_escape(ip) << "\""
             << ",\"backend\":\"" << json_escape(backend) << "\"}"
             ;
        event_log_.write(line.str());
        std::cout << "[Ninj-OS Edge] " << message << "\n";
    }

    std::size_t active_ban_count(Clock::time_point now) const {
        return static_cast<std::size_t>(std::count_if(
            ip_states_.begin(), ip_states_.end(),
            [&](const auto& item) { return is_banned(item.second, now); }));
    }

    void write_top_risk(std::ostream& output, Clock::time_point now) const {
        struct Item { std::string ip; int risk; int level; std::uint64_t offenses;
                      std::uint64_t banned_until; std::string reason; };
        std::vector<Item> items;
        for (const auto& [ip, state] : ip_states_) {
            if (state.risk_score <= 0 && !is_banned(state, now)) continue;
            items.push_back({ip_string(ip), state.risk_score, state.ban_level,
                             state.offenses, state.banned_until_epoch_ms,
                             state.last_reason});
        }
        std::sort(items.begin(), items.end(), [](const Item& a, const Item& b) {
            if (a.risk != b.risk) return a.risk > b.risk;
            return a.offenses > b.offenses;
        });
        if (items.size() > 50) items.resize(50);
        for (std::size_t index = 0; index < items.size(); ++index) {
            const auto& item = items[index];
            output << "      {\"ip\":\"" << json_escape(item.ip)
                   << "\",\"risk\":" << item.risk
                   << ",\"banLevel\":" << item.level
                   << ",\"offenses\":" << item.offenses
                   << ",\"bannedUntil\":" << item.banned_until
                   << ",\"reason\":\"" << json_escape(item.reason) << "\"}"
                   << (index + 1 == items.size() ? "\n" : ",\n");
        }
    }

    void write_state(Clock::time_point now) {
        if (now - last_state_ < std::chrono::milliseconds(config_.state_interval_ms)) {
            return;
        }
        last_state_ = now;

        const auto state_path = runtime_dir_ / "gateway-state.json";
        const auto temporary = runtime_dir_ / "gateway-state.json.tmp";
        std::ofstream output(temporary, std::ios::trunc);
        if (!output) {
            return;
        }

        const double uptime = std::chrono::duration<double>(now - start_time_).count();
        output << "{\n"
               << "  \"timestamp\": " << epoch_ms() << ",\n"
               << "  \"uptimeSeconds\": " << std::fixed << std::setprecision(1) << uptime << ",\n"
               << "  \"listenPort\": " << config_.listen_port << ",\n"
               << "  \"maintenance\": " << (maintenance_ ? "true" : "false") << ",\n"
               << "  \"drain\": " << (drain_ ? "true" : "false") << ",\n"
               << "  \"routingMode\": \"" << json_escape(config_.routing_mode) << "\",\n"
               << "  \"configVersion\": " << config_version_ << ",\n"
               << "  \"lastConfigReload\": " << last_config_reload_epoch_ms_ << ",\n"
               << "  \"lastConfigError\": \"" << json_escape(last_config_error_) << "\",\n"
               << "  \"activeSessions\": " << sessions_.size() << ",\n"
               << "  \"trackedIps\": " << ip_states_.size() << ",\n"
               << "  \"counters\": {\n"
               << "    \"clientPackets\": " << counters_.client_packets << ",\n"
               << "    \"serverPackets\": " << counters_.server_packets << ",\n"
               << "    \"clientBytes\": " << counters_.client_bytes << ",\n"
               << "    \"serverBytes\": " << counters_.server_bytes << ",\n"
               << "    \"droppedPackets\": " << counters_.dropped_packets << ",\n"
               << "    \"rateLimited\": " << counters_.rate_limited << ",\n"
               << "    \"temporaryBans\": " << counters_.temporary_bans << ",\n"
               << "    \"cachedPingReplies\": " << counters_.cached_ping_replies << ",\n"
               << "    \"healthChecks\": " << counters_.health_checks << ",\n"
               << "    \"healthFailures\": " << counters_.health_failures << ",\n"
               << "    \"sessionsOpened\": " << counters_.sessions_opened << ",\n"
               << "    \"sessionsClosed\": " << counters_.sessions_closed << ",\n"
               << "    \"transferTicketsLoaded\": " << counters_.transfer_tickets_loaded << ",\n"
               << "    \"transferTicketsConsumed\": " << counters_.transfer_tickets_consumed << ",\n"
               << "    \"transferTicketRejections\": " << counters_.transfer_ticket_rejections << ",\n"
               << "    \"adaptiveWarnings\": " << counters_.adaptive_warnings << ",\n"
               << "    \"persistentBansLoaded\": " << counters_.persistent_bans_loaded << ",\n"
               << "    \"denylistDrops\": " << counters_.denylist_drops << ",\n"
               << "    \"configReloads\": " << counters_.config_reloads << ",\n"
               << "    \"configReloadFailures\": " << counters_.config_reload_failures << ",\n"
               << "    \"incidentEntries\": " << counters_.incident_entries << ",\n"
               << "    \"incidentExits\": " << counters_.incident_exits << "\n"
               << "  },\n"
               << "  \"incident\": {\n"
               << "    \"enabled\": " << (config_.incident_mode_enabled ? "true" : "false") << ",\n"
               << "    \"active\": " << (incident_mode_ ? "true" : "false") << ",\n"
               << "    \"packetsPerSecond\": " << incident_last_pps_ << ",\n"
               << "    \"dropRatio\": " << std::fixed << std::setprecision(4)
               << incident_last_drop_ratio_ << ",\n"
               << "    \"lastChanged\": " << incident_last_change_epoch_ms_ << "\n"
               << "  },\n"
               << "  \"firewall\": {\n"
               << "    \"enabled\": " << (config_.firewall_enabled ? "true" : "false") << ",\n"
               << "    \"adaptive\": " << (config_.adaptive_firewall_enabled ? "true" : "false") << ",\n"
               << "    \"warningThreshold\": " << config_.risk_warning_threshold << ",\n"
               << "    \"banThreshold\": " << config_.risk_ban_threshold << ",\n"
               << "    \"allowlistCount\": " << allowlisted_ips_.size() << ",\n"
               << "    \"denylistCount\": " << denylisted_ips_.size() << ",\n"
               << "    \"activeBans\": " << active_ban_count(now) << ",\n"
               << "    \"topRisk\": [\n";
        write_top_risk(output, now);
        output << "    ]\n"
               << "  },\n"
               << "  \"staticRoutes\": [\n";
        for (std::size_t index = 0; index < config_.static_routes.size(); ++index) {
            const auto& route = config_.static_routes[index];
            const auto backend_index = backend_index_by_name(route.backend_name);
            const bool available = backend_index && backend_available(*backend_index);
            output << "    {\"listenerPort\":" << route.listener_port
                   << ",\"backend\":\"" << json_escape(route.backend_name)
                   << "\",\"available\":" << (available ? "true" : "false")
                   << "}" << (index + 1 == config_.static_routes.size() ? "\n" : ",\n");
        }
        output << "  ],\n"

               << "  \"transferBroker\": {\n"
               << "    \"enabled\": " << (config_.transfer_enabled ? "true" : "false") << ",\n"
               << "    \"portStart\": " << config_.transfer_port_start << ",\n"
               << "    \"portEnd\": " << config_.transfer_port_end << ",\n"
               << "    \"activeTickets\": " << active_transfer_ticket_count() << "\n"
               << "  },\n"
               << "  \"backends\": [\n";

        for (std::size_t index = 0; index < backends_.size(); ++index) {
            const auto& backend = backends_[index];
            output << "    {\"name\":\"" << json_escape(backend.config.name)
                   << "\",\"host\":\"" << json_escape(backend.config.host)
                   << "\",\"port\":" << backend.config.port
                   << ",\"healthy\":" << (backend.healthy ? "true" : "false")
                   << ",\"enabled\":" << (backend.manual_enabled ? "true" : "false")
                   << ",\"fallback\":" << (backend.config.fallback ? "true" : "false")
                   << ",\"configuredEnabled\":" << (backend.config.enabled ? "true" : "false")
                   << ",\"protectionProfile\":\""
                   << json_escape(backend.config.protection_profile) << "\""
                   << ",\"latencyMs\":" << std::fixed << std::setprecision(2) << backend.latency_ms
                   << ",\"activeSessions\":" << backend.active_sessions
                   << ",\"cachedPing\":" << (!backend.cached_pong.empty() ? "true" : "false")
                   << "}" << (index + 1 == backends_.size() ? "\n" : ",\n");
        }
        output << "  ]\n}\n";
        output.close();
        std::error_code error;
        fs::rename(temporary, state_path, error);
        if (error) {
            fs::remove(state_path, error);
            error.clear();
            fs::rename(temporary, state_path, error);
        }

        write_sessions();
        packet_log_.flush();
        event_log_.flush();
    }

    void write_sessions() {
        const auto path = runtime_dir_ / "sessions.json";
        const auto temporary = runtime_dir_ / "sessions.json.tmp";
        std::ofstream output(temporary, std::ios::trunc);
        if (!output) {
            return;
        }
        output << "[\n";
        std::size_t current = 0;
        for (const auto& [fd, session] : sessions_) {
            const auto age = std::chrono::duration_cast<std::chrono::seconds>(Clock::now() - session.created).count();
            const auto idle = std::chrono::duration_cast<std::chrono::seconds>(Clock::now() - session.last_activity).count();
            output << "  {\"client\":\"" << json_escape(endpoint_string(session.client))
                   << "\",\"ip\":\"" << json_escape(ip_string(session.ip))
                   << "\",\"listenerPort\":" << session.listener_port
                   << ",\"upstreamLocalPort\":" << session.upstream_local_port
                   << ",\"transferTicketId\":\"" << json_escape(session.transfer_ticket_id)
                   << "\",\"backend\":\""
                   << json_escape(backends_[session.backend_index].config.name)
                   << "\",\"ageSeconds\":" << age
                   << ",\"idleSeconds\":" << idle
                   << ",\"clientBytes\":" << session.client_to_server_bytes
                   << ",\"serverBytes\":" << session.server_to_client_bytes
                   << ",\"clientPackets\":" << session.client_packets
                   << ",\"serverPackets\":" << session.server_packets << "}"
                   << (++current == sessions_.size() ? "\n" : ",\n");
        }
        output << "]\n";
        output.close();
        std::error_code error;
        fs::rename(temporary, path, error);
        if (error) {
            fs::remove(path, error);
            error.clear();
            fs::rename(temporary, path, error);
        }
    }

    void print_stats(Clock::time_point now) {
        if (now - last_stats_ < std::chrono::seconds(config_.stats_interval_seconds)) {
            return;
        }
        last_stats_ = now;
        packet_log_.flush();
        event_log_.flush();
        std::cout << "[Ninj-OS Edge] Sessions=" << sessions_.size()
                  << " trackedIPs=" << ip_states_.size()
                  << " c2s=" << counters_.client_bytes
                  << "B s2c=" << counters_.server_bytes
                  << "B dropped=" << counters_.dropped_packets
                  << " cachedPings=" << counters_.cached_ping_replies << "\n";
    }

    void shutdown() {
        if (shutdown_complete_) {
            return;
        }
        shutdown_complete_ = true;
        std::vector<int> session_fds;
        session_fds.reserve(sessions_.size());
        for (const auto& [fd, _] : sessions_) {
            session_fds.push_back(fd);
        }
        for (int fd : session_fds) {
            close_session(fd, "shutdown");
        }
        for (auto& backend : backends_) {
            if (backend.health_fd >= 0) {
                close(backend.health_fd);
                backend.health_fd = -1;
            }
        }
        for (const auto& [fd, port] : listener_fd_to_port_) {
            (void)port;
            epoll_ctl(epoll_fd_, EPOLL_CTL_DEL, fd, nullptr);
            close(fd);
        }
        listener_fd_to_port_.clear();
        listener_port_to_fd_.clear();
        listener_fd_ = -1;
        if (epoll_fd_ >= 0) {
            close(epoll_fd_);
            epoll_fd_ = -1;
        }
        save_persistent_bans();
        packet_log_.flush();
        event_log_.flush();
    }

    Config config_;
    fs::path base_config_path_;
    fs::path runtime_dir_;
    fs::path command_path_;
    fs::path transfer_ticket_path_;
    fs::path live_config_path_;
    fs::path firewall_allowlist_path_;
    fs::path firewall_denylist_path_;
    fs::path firewall_bans_path_;
    fs::path protection_profiles_path_;
    RotatingJsonl packet_log_;
    RotatingJsonl event_log_;

    int listener_fd_{-1};
    int epoll_fd_{-1};
    std::unordered_map<int, std::uint16_t> listener_fd_to_port_;
    std::unordered_map<std::uint16_t, int> listener_port_to_fd_;
    std::vector<Backend> backends_;
    std::unordered_map<int, std::size_t> health_fd_to_backend_;
    std::unordered_map<int, Session> sessions_;
    std::unordered_map<EndpointKey, int, EndpointHash> by_client_;
    std::unordered_map<IpKey, IpState, IpHash> ip_states_;
    std::unordered_set<IpKey, IpHash> allowlisted_ips_;
    std::unordered_set<IpKey, IpHash> denylisted_ips_;
    std::unordered_map<std::string, ProtectionProfile> protection_profiles_;
    std::unordered_map<std::uint16_t, TransferTicket> transfer_tickets_;
    std::unordered_set<std::string> consumed_transfer_ticket_ids_;

    bool maintenance_{};
    bool drain_{};
    bool incident_mode_{};
    bool restart_requested_{};
    int restart_exit_code_{75};
    bool shutdown_complete_{};
    std::size_t round_robin_index_{};
    std::uint64_t command_offset_{};
    std::uint64_t global_packets_in_window_{};
    Counters counters_{};
    std::mt19937_64 random_{};

    Clock::time_point start_time_;
    Clock::time_point last_cleanup_;
    Clock::time_point last_stats_;
    Clock::time_point last_state_;
    Clock::time_point last_command_poll_;
    Clock::time_point last_ticket_reload_;
    Clock::time_point last_live_config_check_;
    Clock::time_point last_incident_check_;
    Clock::time_point incident_last_change_;
    Clock::time_point incident_recovery_candidate_;
    Clock::time_point global_window_;
    fs::file_time_type live_config_mtime_{};
    std::uint64_t config_version_{1};
    std::uint64_t last_config_reload_epoch_ms_{};
    std::uint64_t incident_last_client_packets_{};
    std::uint64_t incident_last_dropped_packets_{};
    std::uint64_t incident_last_change_epoch_ms_{};
    std::size_t incident_last_pps_{};
    double incident_last_drop_ratio_{};
    std::string last_config_error_;
};

void print_help() {
    std::cout << ninjos::product::kName << " v" << ninjos::product::kVersion << "\n"
              << ninjos::product::kEngine << "\n"
              << "Usage: NinjOSEdge --config /home/container/gateway.conf\n";
}

}  // namespace

int main(int argc, char** argv) {
    fs::path config_path = "gateway.conf";
    for (int index = 1; index < argc; ++index) {
        const std::string argument = argv[index];
        if (argument == "--config" && index + 1 < argc) {
            config_path = argv[++index];
        } else if (argument == "--help") {
            print_help();
            return 0;
        } else {
            std::cerr << "Unknown argument: " << argument << "\n";
            return 2;
        }
    }

    std::signal(SIGINT, handle_signal);
    std::signal(SIGTERM, handle_signal);
    std::signal(SIGHUP, handle_signal);

    // Pterodactyl sends the configured stop command through standard input.
    // Keep this separate from the epoll loop so both `stop` and SIGTERM shut
    // the gateway down cleanly.
    std::thread([] {
        std::string command;
        while (g_running.load() && std::getline(std::cin, command)) {
            command = trim(command);
            std::transform(command.begin(), command.end(), command.begin(), [](unsigned char c) {
                return static_cast<char>(std::tolower(c));
            });
            if (command == "stop" || command == "quit" || command == "exit") {
                g_running.store(false);
                break;
            }
        }
    }).detach();

    try {
        Gateway gateway(load_config(config_path), config_path);
        return gateway.run();
    } catch (const std::exception& error) {
        std::cerr << "[Ninj-OS Edge] Fatal error: " << error.what() << "\n";
        return 1;
    }
}
