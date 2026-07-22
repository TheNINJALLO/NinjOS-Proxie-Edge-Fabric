// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

#include <endstone/command/command.h>
#include <endstone/command/command_sender.h>

// Endstone 0.11.x packet event headers use SocketAddress but do not include
// its defining public header themselves. It must be visible before either
// packet event header is parsed.
#include <endstone/util/socket_address.h>

#include <endstone/event/player/player_join_event.h>
#include <endstone/event/server/packet_receive_event.h>
#include <endstone/event/server/packet_send_event.h>
#include <endstone/permissions/permission.h>
#include <endstone/player.h>
#include <endstone/plugin/plugin.h>
#include <endstone/scheduler/scheduler.h>
#include <endstone/server.h>

#include <arpa/inet.h>
#include <netdb.h>
#include <fcntl.h>
#include <poll.h>
#include <sys/socket.h>
#include <unistd.h>

#include <algorithm>
#include <array>
#include <atomic>
#include <chrono>
#include <cctype>
#include <cerrno>
#include <condition_variable>
#include <cstdint>
#include <cstring>
#include <deque>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <mutex>
#include <optional>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>
#include <unordered_map>
#include <unordered_set>
#include <utility>
#include <vector>

namespace fs = std::filesystem;
using Clock = std::chrono::steady_clock;

namespace {

std::string trim(std::string value) {
    const auto first = value.find_first_not_of(" \t\r\n");
    if (first == std::string::npos) return {};
    const auto last = value.find_last_not_of(" \t\r\n");
    return value.substr(first, last - first + 1);
}

std::string lower(std::string value) {
    std::transform(value.begin(), value.end(), value.begin(), [](unsigned char c) {
        return static_cast<char>(std::tolower(c));
    });
    return value;
}

bool parse_bool(const std::string &value, bool fallback) {
    const auto normalized = lower(trim(value));
    if (normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on") return true;
    if (normalized == "false" || normalized == "0" || normalized == "no" || normalized == "off") return false;
    return fallback;
}

std::size_t parse_size(const std::string &value, std::size_t fallback) {
    try {
        return static_cast<std::size_t>(std::stoull(trim(value)));
    } catch (...) {
        return fallback;
    }
}

std::unordered_set<int> parse_ids(const std::string &value) {
    std::unordered_set<int> output;
    std::stringstream stream(value);
    std::string token;
    while (std::getline(stream, token, ',')) {
        token = trim(token);
        if (token.empty()) continue;
        try {
            output.insert(std::stoi(token));
        } catch (...) {}
    }
    return output;
}

std::unordered_map<std::string, std::string> load_properties(const fs::path &path) {
    std::unordered_map<std::string, std::string> values;
    std::ifstream input(path);
    std::string line;
    while (std::getline(input, line)) {
        line = trim(line);
        if (line.empty() || line.front() == '#' || line.front() == ';') continue;
        const auto equal = line.find('=');
        if (equal == std::string::npos) continue;
        values[trim(line.substr(0, equal))] = trim(line.substr(equal + 1));
    }
    return values;
}

std::string property(const std::unordered_map<std::string, std::string> &values,
                     const std::string &key,
                     const std::string &fallback) {
    const auto found = values.find(key);
    return found == values.end() ? fallback : found->second;
}

std::uint64_t epoch_ms() {
    return static_cast<std::uint64_t>(std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::system_clock::now().time_since_epoch()).count());
}

std::string json_escape(std::string_view value) {
    std::ostringstream output;
    for (const unsigned char c : value) {
        switch (c) {
            case '\\': output << "\\\\"; break;
            case '"': output << "\\\""; break;
            case '\n': output << "\\n"; break;
            case '\r': output << "\\r"; break;
            case '\t': output << "\\t"; break;
            default:
                if (c < 0x20) {
                    output << "\\u" << std::hex << std::setw(4) << std::setfill('0')
                           << static_cast<int>(c) << std::dec;
                } else {
                    output << static_cast<char>(c);
                }
        }
    }
    return output.str();
}

std::string hex_preview(std::string_view payload, std::size_t limit) {
    static constexpr char digits[] = "0123456789abcdef";
    const std::size_t count = std::min(limit, payload.size());
    std::string output;
    output.resize(count * 2);
    for (std::size_t i = 0; i < count; ++i) {
        const auto byte = static_cast<unsigned char>(payload[i]);
        output[i * 2] = digits[byte >> 4];
        output[i * 2 + 1] = digits[byte & 0x0f];
    }
    return output;
}

// Compact SHA-256 implementation used only to HMAC dashboard batches. This
// keeps the plugin self-contained and avoids runtime OpenSSL dependencies in
// the Bedrock container.
class Sha256 {
public:
    Sha256() { reset(); }

    void reset() {
        bit_length_ = 0;
        buffer_length_ = 0;
        state_ = {0x6a09e667u, 0xbb67ae85u, 0x3c6ef372u, 0xa54ff53au,
                  0x510e527fu, 0x9b05688cu, 0x1f83d9abu, 0x5be0cd19u};
    }

    void update(const std::uint8_t *data, std::size_t length) {
        for (std::size_t i = 0; i < length; ++i) {
            buffer_[buffer_length_++] = data[i];
            if (buffer_length_ == 64) {
                transform(buffer_.data());
                bit_length_ += 512;
                buffer_length_ = 0;
            }
        }
    }

    void update(std::string_view value) {
        update(reinterpret_cast<const std::uint8_t *>(value.data()), value.size());
    }

    std::array<std::uint8_t, 32> final() {
        std::array<std::uint8_t, 32> hash{};
        std::size_t i = buffer_length_;
        buffer_[i++] = 0x80;
        if (i > 56) {
            while (i < 64) buffer_[i++] = 0;
            transform(buffer_.data());
            i = 0;
        }
        while (i < 56) buffer_[i++] = 0;
        bit_length_ += buffer_length_ * 8;
        for (int shift = 56; shift >= 0; shift -= 8) {
            buffer_[i++] = static_cast<std::uint8_t>((bit_length_ >> shift) & 0xff);
        }
        transform(buffer_.data());
        for (std::size_t word = 0; word < 8; ++word) {
            hash[word * 4] = static_cast<std::uint8_t>((state_[word] >> 24) & 0xff);
            hash[word * 4 + 1] = static_cast<std::uint8_t>((state_[word] >> 16) & 0xff);
            hash[word * 4 + 2] = static_cast<std::uint8_t>((state_[word] >> 8) & 0xff);
            hash[word * 4 + 3] = static_cast<std::uint8_t>(state_[word] & 0xff);
        }
        return hash;
    }

private:
    static constexpr std::array<std::uint32_t, 64> constants_ = {
        0x428a2f98u,0x71374491u,0xb5c0fbcfu,0xe9b5dba5u,0x3956c25bu,0x59f111f1u,0x923f82a4u,0xab1c5ed5u,
        0xd807aa98u,0x12835b01u,0x243185beu,0x550c7dc3u,0x72be5d74u,0x80deb1feu,0x9bdc06a7u,0xc19bf174u,
        0xe49b69c1u,0xefbe4786u,0x0fc19dc6u,0x240ca1ccu,0x2de92c6fu,0x4a7484aau,0x5cb0a9dcu,0x76f988dau,
        0x983e5152u,0xa831c66du,0xb00327c8u,0xbf597fc7u,0xc6e00bf3u,0xd5a79147u,0x06ca6351u,0x14292967u,
        0x27b70a85u,0x2e1b2138u,0x4d2c6dfcu,0x53380d13u,0x650a7354u,0x766a0abbu,0x81c2c92eu,0x92722c85u,
        0xa2bfe8a1u,0xa81a664bu,0xc24b8b70u,0xc76c51a3u,0xd192e819u,0xd6990624u,0xf40e3585u,0x106aa070u,
        0x19a4c116u,0x1e376c08u,0x2748774cu,0x34b0bcb5u,0x391c0cb3u,0x4ed8aa4au,0x5b9cca4fu,0x682e6ff3u,
        0x748f82eeu,0x78a5636fu,0x84c87814u,0x8cc70208u,0x90befffau,0xa4506cebu,0xbef9a3f7u,0xc67178f2u};

    static std::uint32_t rotate(std::uint32_t value, std::uint32_t bits) {
        return (value >> bits) | (value << (32 - bits));
    }

    void transform(const std::uint8_t *data) {
        std::array<std::uint32_t, 64> words{};
        for (std::size_t i = 0; i < 16; ++i) {
            words[i] = (static_cast<std::uint32_t>(data[i * 4]) << 24) |
                       (static_cast<std::uint32_t>(data[i * 4 + 1]) << 16) |
                       (static_cast<std::uint32_t>(data[i * 4 + 2]) << 8) |
                       static_cast<std::uint32_t>(data[i * 4 + 3]);
        }
        for (std::size_t i = 16; i < 64; ++i) {
            const auto s0 = rotate(words[i - 15], 7) ^ rotate(words[i - 15], 18) ^ (words[i - 15] >> 3);
            const auto s1 = rotate(words[i - 2], 17) ^ rotate(words[i - 2], 19) ^ (words[i - 2] >> 10);
            words[i] = words[i - 16] + s0 + words[i - 7] + s1;
        }
        auto a = state_[0], b = state_[1], c = state_[2], d = state_[3];
        auto e = state_[4], f = state_[5], g = state_[6], h = state_[7];
        for (std::size_t i = 0; i < 64; ++i) {
            const auto s1 = rotate(e, 6) ^ rotate(e, 11) ^ rotate(e, 25);
            const auto choice = (e & f) ^ ((~e) & g);
            const auto temp1 = h + s1 + choice + constants_[i] + words[i];
            const auto s0 = rotate(a, 2) ^ rotate(a, 13) ^ rotate(a, 22);
            const auto majority = (a & b) ^ (a & c) ^ (b & c);
            const auto temp2 = s0 + majority;
            h = g; g = f; f = e; e = d + temp1; d = c; c = b; b = a; a = temp1 + temp2;
        }
        state_[0] += a; state_[1] += b; state_[2] += c; state_[3] += d;
        state_[4] += e; state_[5] += f; state_[6] += g; state_[7] += h;
    }

    std::array<std::uint32_t, 8> state_{};
    std::array<std::uint8_t, 64> buffer_{};
    std::uint64_t bit_length_{};
    std::size_t buffer_length_{};
};

std::string hex_digest(const std::array<std::uint8_t, 32> &digest) {
    static constexpr char digits[] = "0123456789abcdef";
    std::string output(64, '0');
    for (std::size_t i = 0; i < digest.size(); ++i) {
        output[i * 2] = digits[digest[i] >> 4];
        output[i * 2 + 1] = digits[digest[i] & 0x0f];
    }
    return output;
}

std::string secret_fingerprint(std::string_view value) {
    Sha256 sha;
    sha.update(value);
    auto digest = hex_digest(sha.final()).substr(0, 12);
    std::transform(digest.begin(), digest.end(), digest.begin(), [](unsigned char c) {
        return static_cast<char>(std::toupper(c));
    });
    return digest;
}

std::string hmac_sha256(std::string_view key, std::string_view message) {
    std::array<std::uint8_t, 64> key_block{};
    if (key.size() > key_block.size()) {
        Sha256 sha;
        sha.update(key);
        const auto digest = sha.final();
        std::copy(digest.begin(), digest.end(), key_block.begin());
    } else {
        std::copy(key.begin(), key.end(), key_block.begin());
    }
    std::array<std::uint8_t, 64> inner{}, outer{};
    for (std::size_t i = 0; i < key_block.size(); ++i) {
        inner[i] = key_block[i] ^ 0x36;
        outer[i] = key_block[i] ^ 0x5c;
    }
    Sha256 inner_sha;
    inner_sha.update(inner.data(), inner.size());
    inner_sha.update(message);
    const auto inner_digest = inner_sha.final();
    Sha256 outer_sha;
    outer_sha.update(outer.data(), outer.size());
    outer_sha.update(inner_digest.data(), inner_digest.size());
    return hex_digest(outer_sha.final());
}


std::uint64_t read_uint_file(const fs::path &path) {
    std::ifstream input(path);
    std::string value;
    input >> value;
    if (value.empty() || value == "max") return 0;
    try { return std::stoull(value); } catch (...) { return 0; }
}

std::uint64_t process_rss_bytes() {
    std::ifstream input("/proc/self/statm");
    std::uint64_t pages = 0;
    std::uint64_t resident = 0;
    input >> pages >> resident;
    return resident * static_cast<std::uint64_t>(sysconf(_SC_PAGESIZE));
}

std::uint64_t process_cpu_ticks() {
    std::ifstream input("/proc/self/stat");
    std::string line;
    std::getline(input, line);
    const auto close = line.rfind(')');
    if (close == std::string::npos) return 0;
    std::istringstream fields(line.substr(close + 2));
    std::string value;
    std::vector<std::string> parts;
    while (fields >> value) parts.push_back(value);
    // Fields after the command start at Linux proc field 3. utime/stime are 14/15.
    if (parts.size() <= 12) return 0;
    try { return std::stoull(parts[11]) + std::stoull(parts[12]); } catch (...) { return 0; }
}

std::pair<std::uint64_t, std::uint64_t> container_network_bytes() {
    std::ifstream input("/proc/net/dev");
    std::string line;
    std::uint64_t rx = 0;
    std::uint64_t tx = 0;
    while (std::getline(input, line)) {
        const auto colon = line.find(':');
        if (colon == std::string::npos) continue;
        const auto interface = trim(line.substr(0, colon));
        if (interface == "lo") continue;
        std::istringstream values(line.substr(colon + 1));
        std::array<std::uint64_t, 16> fields{};
        for (auto &field : fields) values >> field;
        rx += fields[0];
        tx += fields[8];
    }
    return {rx, tx};
}

struct CompanionConfig {
    std::string dashboard_host{"185.83.152.144"};
    std::uint16_t dashboard_port{25571};
    std::string shared_secret{"CHANGE_ME_NOW"};
    std::string server_id{"kingdom"};
    std::string capture_mode{"metadata"};
    std::unordered_set<int> selected_packet_ids{};
    std::unordered_set<int> redact_packet_ids{1, 3, 4};
    std::unordered_set<int> drop_receive_ids{};
    std::unordered_set<int> drop_send_ids{};
    std::size_t payload_limit{512};
    std::size_t queue_capacity{50000};
    std::size_t batch_size{200};
    int flush_ms{100};
    std::size_t movement_sample_rate{20};
    int metrics_interval_ticks{20};
    int reconnect_seconds{3};
    bool transfer_enabled{true};
    bool presence_enabled{true};
    bool presence_include_address{false};
    bool identity_bridge_enabled{true};
    bool require_proxy_identity{true};
};

CompanionConfig load_config(const fs::path &path) {
    const auto values = load_properties(path);
    CompanionConfig cfg;
    cfg.dashboard_host = trim(property(values, "dashboard_host", cfg.dashboard_host));
    cfg.dashboard_port = static_cast<std::uint16_t>(parse_size(property(values, "dashboard_port", "25571"), 25571));
    cfg.shared_secret = trim(property(values, "shared_secret", cfg.shared_secret));
    cfg.server_id = lower(trim(property(values, "server_id", cfg.server_id)));
    cfg.capture_mode = lower(property(values, "capture_mode", cfg.capture_mode));
    cfg.selected_packet_ids = parse_ids(property(values, "selected_packet_ids", ""));
    cfg.redact_packet_ids = parse_ids(property(values, "redact_packet_ids", "1,3,4"));
    cfg.drop_receive_ids = parse_ids(property(values, "drop_receive_ids", ""));
    cfg.drop_send_ids = parse_ids(property(values, "drop_send_ids", ""));
    cfg.payload_limit = parse_size(property(values, "payload_limit", "512"), 512);
    cfg.queue_capacity = parse_size(property(values, "queue_capacity", "50000"), 50000);
    cfg.batch_size = parse_size(property(values, "batch_size", "200"), 200);
    cfg.flush_ms = static_cast<int>(parse_size(property(values, "flush_ms", "100"), 100));
    cfg.movement_sample_rate = std::max<std::size_t>(1, parse_size(property(values, "movement_sample_rate", "20"), 20));
    cfg.metrics_interval_ticks = static_cast<int>(parse_size(property(values, "metrics_interval_ticks", "20"), 20));
    cfg.reconnect_seconds = static_cast<int>(parse_size(property(values, "reconnect_seconds", "3"), 3));
    cfg.transfer_enabled = parse_bool(property(values, "transfer_enabled", "true"), true);
    cfg.presence_enabled = parse_bool(property(values, "presence_enabled", "true"), true);
    cfg.presence_include_address = parse_bool(property(values, "presence_include_address", "false"), false);
    cfg.identity_bridge_enabled = parse_bool(property(values, "identity_bridge_enabled", "true"), true);
    cfg.require_proxy_identity = parse_bool(property(values, "require_proxy_identity", "true"), true);
    if (cfg.dashboard_port == 0) cfg.dashboard_port = 25571;
    if (cfg.capture_mode != "off" && cfg.capture_mode != "metadata" &&
        cfg.capture_mode != "selected" && cfg.capture_mode != "all") {
        cfg.capture_mode = "metadata";
    }
    return cfg;
}

void write_default_config(const fs::path &path) {
    std::ofstream output(path);
    output << "# Ninj-OS Proxie Endstone Companion\n"
           << "dashboard_host=185.83.152.144\n"
           << "dashboard_port=25571\n"
           << "shared_secret=CHANGE_ME_NOW\n"
           << "server_id=kingdom\n"
           << "# Full Proxy Mode identity restoration. Disable both in transparent online-mode=true mode.\n"
           << "identity_bridge_enabled=true\n"
           << "require_proxy_identity=true\n"
           << "# off | metadata | selected | all\n"
           << "capture_mode=metadata\n"
           << "selected_packet_ids=30,77\n"
           << "payload_limit=512\n"
           << "# Login and encryption handshake payloads are always redacted.\n"
           << "redact_packet_ids=1,3,4\n"
           << "queue_capacity=50000\n"
           << "batch_size=200\n"
           << "flush_ms=100\n"
           << "movement_sample_rate=20\n"
           << "metrics_interval_ticks=20\n"
           << "# Report online player name/XUID heartbeats to the network presence map.\n"
           << "presence_enabled=true\n"
           << "# Keep false unless staff explicitly need player network addresses.\n"
           << "presence_include_address=false\n"
           << "reconnect_seconds=3\n"
           << "# Enables /ninjosserver <backend> transfer-ticket requests.\n"
           << "transfer_enabled=true\n"
           << "# Advanced packet rules. IDs listed here are cancelled.\n"
           << "drop_receive_ids=\n"
           << "drop_send_ids=\n";
}

struct Record {
    std::string json;
};

bool connect_with_timeout(int socket_fd,
                          const sockaddr *address,
                          socklen_t address_length,
                          int timeout_ms,
                          std::string &error) {
    const int original_flags = fcntl(socket_fd, F_GETFL, 0);
    if (original_flags < 0) {
        error = std::strerror(errno);
        return false;
    }
    if (fcntl(socket_fd, F_SETFL, original_flags | O_NONBLOCK) < 0) {
        error = std::strerror(errno);
        return false;
    }

    const int result = connect(socket_fd, address, address_length);
    if (result == 0) {
        (void)fcntl(socket_fd, F_SETFL, original_flags);
        return true;
    }
    if (errno != EINPROGRESS) {
        error = std::strerror(errno);
        (void)fcntl(socket_fd, F_SETFL, original_flags);
        return false;
    }

    pollfd descriptor{socket_fd, POLLOUT, 0};
    const int polled = poll(&descriptor, 1, timeout_ms);
    if (polled <= 0) {
        error = polled == 0 ? "Dashboard connection timed out" : std::strerror(errno);
        (void)fcntl(socket_fd, F_SETFL, original_flags);
        return false;
    }

    int socket_error = 0;
    socklen_t error_length = sizeof(socket_error);
    if (getsockopt(socket_fd, SOL_SOCKET, SO_ERROR, &socket_error, &error_length) < 0) {
        error = std::strerror(errno);
        (void)fcntl(socket_fd, F_SETFL, original_flags);
        return false;
    }
    if (socket_error != 0) {
        error = std::strerror(socket_error);
        (void)fcntl(socket_fd, F_SETFL, original_flags);
        return false;
    }

    (void)fcntl(socket_fd, F_SETFL, original_flags);
    return true;
}

class HttpClient {
public:
    struct TransferResponse {
        std::string host;
        std::uint16_t port{};
        std::string ticket_id;
        std::uint64_t expires_at{};
    };

    static bool post(const CompanionConfig &config, const std::string &body, std::string &error) {
        std::string response_body;
        return post_path(config, "/ingest", body, response_body, error);
    }

    static bool probe(const CompanionConfig &config, std::string &error) {
        std::ostringstream body;
        body << "{\"serverId\":\"" << json_escape(config.server_id)
             << "\",\"records\":[{\"type\":\"event\",\"timestamp\":" << epoch_ms()
             << ",\"eventType\":\"companion.probe\",\"severity\":\"info\","
             << "\"message\":\"Manual companion connectivity probe\"}]}";
        std::string response_body;
        return post_path(config, "/ingest", body.str(), response_body, error);
    }

    static bool request_transfer(const CompanionConfig &config,
                                 const std::string &body,
                                 TransferResponse &response,
                                 std::string &error) {
        std::string response_body;
        if (!post_path(config, "/transfer", body, response_body, error)) {
            return false;
        }
        response.host = json_string(response_body, "host");
        response.ticket_id = json_string(response_body, "ticketId");
        response.port = static_cast<std::uint16_t>(json_unsigned(response_body, "port"));
        response.expires_at = json_unsigned(response_body, "expiresAt");
        if (response.host.empty() || response.port == 0) {
            error = "Dashboard returned an invalid transfer route";
            return false;
        }
        return true;
    }

    struct IdentityResponse {
        std::string username;
        std::string xuid;
        std::string role;
        bool operator_status{};
        std::vector<std::string> permissions;
    };

    static bool consume_identity(const CompanionConfig &config,
                                 const std::string &username,
                                 IdentityResponse &identity,
                                 std::string &error) {
        std::ostringstream body;
        body << "{\"serverId\":\"" << json_escape(config.server_id)
             << "\",\"username\":\"" << json_escape(username)
             << "\",\"sessionId\":\"\"}";
        std::string response_body;
        if (!post_path(config, "/api/bridge/v1/join/consume", body.str(), response_body, error)) {
            return false;
        }
        identity.username = json_string(response_body, "username");
        identity.xuid = json_string(response_body, "xuid");
        identity.role = json_string(response_body, "role");
        identity.operator_status = json_boolean(response_body, "operator");
        identity.permissions = json_string_array(response_body, "permissions");
        if (identity.username.empty()) {
            error = "Dashboard returned an invalid identity grant";
            return false;
        }
        return true;
    }

private:
    static std::string json_string(const std::string &json, const std::string &name) {
        const std::string marker = "\"" + name + "\"";
        auto position = json.find(marker);
        if (position == std::string::npos) return {};
        position = json.find(':', position + marker.size());
        if (position == std::string::npos) return {};
        position = json.find('"', position + 1);
        if (position == std::string::npos) return {};
        std::string output;
        bool escaped = false;
        for (++position; position < json.size(); ++position) {
            const char value = json[position];
            if (escaped) {
                switch (value) {
                    case 'n': output.push_back('\n'); break;
                    case 'r': output.push_back('\r'); break;
                    case 't': output.push_back('\t'); break;
                    default: output.push_back(value); break;
                }
                escaped = false;
                continue;
            }
            if (value == '\\') {
                escaped = true;
                continue;
            }
            if (value == '"') break;
            output.push_back(value);
        }
        return output;
    }


    static bool json_boolean(const std::string &json, const std::string &name) {
        const std::string marker = "\"" + name + "\"";
        auto position = json.find(marker);
        if (position == std::string::npos) return false;
        position = json.find(':', position + marker.size());
        if (position == std::string::npos) return false;
        ++position;
        while (position < json.size() && std::isspace(static_cast<unsigned char>(json[position]))) ++position;
        return json.compare(position, 4, "true") == 0;
    }

    static std::vector<std::string> json_string_array(const std::string &json, const std::string &name) {
        std::vector<std::string> values;
        const std::string marker = "\"" + name + "\"";
        auto position = json.find(marker);
        if (position == std::string::npos) return values;
        position = json.find('[', position + marker.size());
        const auto end = json.find(']', position);
        if (position == std::string::npos || end == std::string::npos) return values;
        while (position < end) {
            position = json.find('\"', position + 1);
            if (position == std::string::npos || position >= end) break;
            const auto close = json.find('\"', position + 1);
            if (close == std::string::npos || close > end) break;
            values.push_back(json.substr(position + 1, close - position - 1));
            position = close;
        }
        return values;
    }

    static std::uint64_t json_unsigned(const std::string &json, const std::string &name) {
        const std::string marker = "\"" + name + "\"";
        auto position = json.find(marker);
        if (position == std::string::npos) return 0;
        position = json.find(':', position + marker.size());
        if (position == std::string::npos) return 0;
        ++position;
        while (position < json.size() && std::isspace(static_cast<unsigned char>(json[position]))) ++position;
        const auto begin = position;
        while (position < json.size() && std::isdigit(static_cast<unsigned char>(json[position]))) ++position;
        if (begin == position) return 0;
        try { return std::stoull(json.substr(begin, position - begin)); } catch (...) { return 0; }
    }

    static bool post_path(const CompanionConfig &config,
                          const std::string &path,
                          const std::string &body,
                          std::string &response_body,
                          std::string &error) {
        addrinfo hints{};
        hints.ai_family = AF_UNSPEC;
        hints.ai_socktype = SOCK_STREAM;
        addrinfo *result = nullptr;
        const auto port = std::to_string(config.dashboard_port);
        const int lookup = getaddrinfo(config.dashboard_host.c_str(), port.c_str(), &hints, &result);
        if (lookup != 0) {
            error = gai_strerror(lookup);
            return false;
        }
        int socket_fd = -1;
        for (auto *entry = result; entry != nullptr; entry = entry->ai_next) {
            socket_fd = socket(entry->ai_family, entry->ai_socktype, entry->ai_protocol);
            if (socket_fd < 0) continue;
            timeval timeout{2, 0};
            setsockopt(socket_fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout));
            setsockopt(socket_fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout));
            std::string connect_error;
            if (connect_with_timeout(socket_fd, entry->ai_addr, entry->ai_addrlen, 2000, connect_error)) break;
            error = connect_error;
            close(socket_fd);
            socket_fd = -1;
        }
        freeaddrinfo(result);
        if (socket_fd < 0) {
            if (error.empty()) error = std::strerror(errno);
            return false;
        }

        const auto timestamp = std::to_string(epoch_ms());
        const auto signature = hmac_sha256(config.shared_secret, timestamp + "\n" + body);
        std::ostringstream request;
        request << "POST " << path << " HTTP/1.1\r\n"
                << "Host: " << config.dashboard_host << ':' << config.dashboard_port << "\r\n"
                << "Content-Type: application/json\r\n"
                << "Content-Length: " << body.size() << "\r\n"
                << "X-NinjOS-Timestamp: " << timestamp << "\r\n"
                << "X-NinjOS-Signature: " << signature << "\r\n"
                << "X-NinjOS-Server: " << config.server_id << "\r\n"
                << "Connection: close\r\n\r\n"
                << body;
        const auto payload = request.str();
        std::size_t offset = 0;
        while (offset < payload.size()) {
            const auto written = send(socket_fd, payload.data() + offset, payload.size() - offset, MSG_NOSIGNAL);
            if (written <= 0) {
                error = std::strerror(errno);
                close(socket_fd);
                return false;
            }
            offset += static_cast<std::size_t>(written);
        }

        std::string response;
        std::array<char, 4096> buffer{};
        while (response.size() < 65536) {
            const auto received = recv(socket_fd, buffer.data(), buffer.size(), 0);
            if (received > 0) {
                response.append(buffer.data(), static_cast<std::size_t>(received));
                continue;
            }
            if (received == 0) break;
            if (errno == EINTR) continue;
            break;
        }
        close(socket_fd);
        if (response.empty()) {
            error = "Dashboard returned no response";
            return false;
        }
        const auto status_end = response.find("\r\n");
        const auto status = response.substr(0, status_end);
        if (status.find(" 200 ") == std::string::npos && status.find(" 201 ") == std::string::npos) {
            const auto body_start = response.find("\r\n\r\n");
            error = body_start == std::string::npos ? status : response.substr(body_start + 4);
            return false;
        }
        const auto body_start = response.find("\r\n\r\n");
        response_body = body_start == std::string::npos ? std::string{} : response.substr(body_start + 4);
        return true;
    }
};
} // namespace

class NinjOSProxieCompanion : public endstone::Plugin {
public:
    void onEnable() override {
        fs::create_directories(getDataFolder());
        config_path_ = getDataFolder() / "companion.properties";
        if (!fs::exists(config_path_)) write_default_config(config_path_);
        reload_config();

        registerEvent(&NinjOSProxieCompanion::on_join, *this,
                      endstone::EventPriority::Highest, false);
        registerEvent(&NinjOSProxieCompanion::on_receive, *this,
                      endstone::EventPriority::Highest, false);
        registerEvent(&NinjOSProxieCompanion::on_send, *this,
                      endstone::EventPriority::Highest, false);

        last_process_ticks_ = process_cpu_ticks();
        last_metrics_at_ = Clock::now();
        running_.store(true);
        worker_ = std::thread([this] { upload_loop(); });
        getServer().getScheduler().runTaskTimer(
            *this, [this] { capture_metrics(); }, 20,
            static_cast<std::uint64_t>(std::max(1, config_.metrics_interval_ticks)));

        enqueue_event("companion.enabled", "Ninj-OS Edge Fabric companion v3.6.1 enabled", "info");
        getLogger().info("Ninj-OS Edge Fabric Companion v3.6.1 enabled. Dashboard {}:{} serverId={} secretFingerprint={}",
                         config_.dashboard_host, config_.dashboard_port, config_.server_id,
                         secret_fingerprint(config_.shared_secret));
        if (config_.shared_secret == "CHANGE_ME_NOW") {
            getLogger().warning("Change shared_secret in {} before exposing the dashboard.", config_path_.string());
        }
    }

    void onDisable() override {
        enqueue_event("companion.disabled", "Ninj-OS packet companion disabled", "warning");
        running_.store(false);
        queue_cv_.notify_all();
        if (worker_.joinable()) worker_.join();
        getLogger().info("Ninj-OS Edge Fabric Companion v3.6.1 disabled.");
    }

    bool onCommand(endstone::CommandSender &sender,
                   const endstone::Command &command,
                   const std::vector<std::string> &args) override {
        if (command.getName() == "ninjosproxiemonitor") {
            const auto action = args.empty() ? "status" : lower(args.front());
            if (action == "reload") {
                reload_config();
                const auto config = config_snapshot();
                sender.sendMessage("Ninj-OS companion configuration reloaded: target={}:{} server={} secretFingerprint={}",
                                   config.dashboard_host, config.dashboard_port, config.server_id,
                                   secret_fingerprint(config.shared_secret));
                return true;
            }
            if (action == "probe") {
                const auto config = config_snapshot();
                std::string error;
                if (HttpClient::probe(config, error)) {
                    record_upload_success(1);
                    sender.sendMessage("Companion probe accepted by {}:{} for server {}. Secret fingerprint {}.",
                                       config.dashboard_host, config.dashboard_port, config.server_id,
                                       secret_fingerprint(config.shared_secret));
                } else {
                    record_upload_failure(error);
                    sender.sendErrorMessage("Companion probe failed: {}", error);
                }
                return true;
            }
            if (action == "status") {
                const auto config = config_snapshot();
                const auto last_ok = last_upload_ok_.load();
                const auto last_attempt = last_upload_attempt_.load();
                sender.sendMessage("Ninj-OS Companion v3.6.1: target={}:{} server={} secretFingerprint={}",
                                   config.dashboard_host, config.dashboard_port, config.server_id,
                                   secret_fingerprint(config.shared_secret));
                sender.sendMessage("players={} queue={} dropped={} uploaded={} failures={} capture={} presence={} transfers={}",
                                   getServer().getOnlinePlayers().size(), queue_size(), dropped_.load(),
                                   uploaded_.load(), failures_.load(), config.capture_mode,
                                   config.presence_enabled ? "enabled" : "disabled",
                                   config.transfer_enabled ? "enabled" : "disabled");
                sender.sendMessage("lastAttempt={} lastSuccess={} lastError={}",
                                   last_attempt == 0 ? "never" : std::to_string((epoch_ms() - last_attempt) / 1000) + "s ago",
                                   last_ok == 0 ? "never" : std::to_string((epoch_ms() - last_ok) / 1000) + "s ago",
                                   last_upload_error().empty() ? "none" : last_upload_error());
                return true;
            }
            sender.sendErrorMessage("Usage: /ninjosproxiemonitor [status|reload|probe]");
            return false;
        }

        if (command.getName() == "ninjosserver") {
            auto *player = dynamic_cast<endstone::Player *>(&sender);
            if (player == nullptr) {
                sender.sendErrorMessage("This command can only be used by a player.");
                return true;
            }
            const auto config = config_snapshot();
            if (!config.transfer_enabled) {
                sender.sendErrorMessage("Ninj-OS transfer requests are disabled on this server.");
                return true;
            }
            if (args.empty()) {
                sender.sendErrorMessage("Usage: /ninjosserver <backend>");
                return true;
            }
            const auto destination = lower(trim(args.front()));
            if (destination.empty()) {
                sender.sendErrorMessage("A destination backend is required.");
                return true;
            }

            sender.sendMessage("Requesting a protected route to {}...", destination);
            const auto source_ip = player->getAddress().getHostname();
            std::ostringstream body;
            body << "{\"destination\":\"" << json_escape(destination) << "\""
                 << ",\"sourceIp\":\"" << json_escape(source_ip) << "\""
                 << ",\"sourcePort\":" << player->getAddress().getPort()
                 << ",\"xuid\":\"" << json_escape(player->getXuid()) << "\""
                 << ",\"playerName\":\"" << json_escape(player->getName()) << "\""
                 << ",\"sourceServer\":\"" << json_escape(config.server_id) << "\"}";

            HttpClient::TransferResponse route;
            std::string error;
            if (!HttpClient::request_transfer(config, body.str(), route, error)) {
                sender.sendErrorMessage("Transfer unavailable: {}", error);
                enqueue_event("transfer.failed", "Transfer request failed for " + player->getName() +
                              " destination=" + destination + " reason=" + error, "warning",
                              player->getName(), player->getXuid());
                return true;
            }

            enqueue_event("transfer.started", "Transfer started ticket=" + route.ticket_id +
                          " player=" + player->getName() +
                          " destination=" + destination + " route=" + route.host + ':' +
                          std::to_string(route.port), "info", player->getName(), player->getXuid());
            sender.sendMessage("Transferring to {} through Ninj-OS Proxie...", destination);
            player->transfer(route.host, route.port);
            return true;
        }
        return false;
    }

    void on_join(endstone::PlayerJoinEvent &event) {
        const auto config = config_snapshot();
        if (!config.identity_bridge_enabled) return;
        const auto player_name = event.getPlayer().getName();
        getServer().getScheduler().runTaskAsync(*this, [this, config, player_name] {
            HttpClient::IdentityResponse identity;
            std::string error;
            bool accepted = false;
            // Session Core and the upstream Endstone login complete on separate
            // event loops. Endstone can emit PlayerJoinEvent a few milliseconds
            // before Session Core's one-use grant reaches the dashboard. Retry
            // only that transient "grant not ready" response; authentication and
            // configuration failures must still fail immediately.
            for (int attempt = 0; attempt < 30; ++attempt) {
                accepted = HttpClient::consume_identity(config, player_name, identity, error);
                if (accepted || error.find("No valid proxy identity grant") == std::string::npos) break;
                std::this_thread::sleep_for(std::chrono::milliseconds(100));
            }
            getServer().getScheduler().runTask(*this, [this, config, player_name, identity, error, accepted] {
                auto *player = getServer().getPlayer(player_name);
                if (player == nullptr) return;
                if (!accepted) {
                    getLogger().warning("Proxy identity rejected for {}: {}", player_name, error);
                    if (config.require_proxy_identity) {
                        player->kick("This backend requires a verified Ninj-OS Proxie connection.");
                    }
                    return;
                }
                // A network grant may elevate a player, but a default/member
                // grant must not erase operator status already configured on the
                // Endstone backend.
                if (identity.operator_status) player->setOp(true);
                for (const auto &permission : identity.permissions) {
                    if (!permission.empty() && permission != "*") player->addAttachment(*this, permission, true);
                }
                player->recalculatePermissions();
                player->updateCommands();
                getServer().getScheduler().runTaskLater(*this, [this, player_name, operator_status = identity.operator_status] {
                    auto *current = getServer().getPlayer(player_name);
                    if (current == nullptr) return;
                    if (operator_status) current->setOp(true);
                    current->recalculatePermissions();
                    current->updateCommands();
                }, 2);
                enqueue_event("identity.verified", "Verified proxy identity for " + player_name +
                              " role=" + identity.role + " xuid=" + identity.xuid, "info",
                              player_name, identity.xuid);
            });
        });
    }

private:
    void reload_config() {
        std::scoped_lock lock(config_mutex_);
        config_ = load_config(config_path_);
    }

    CompanionConfig config_snapshot() const {
        std::scoped_lock lock(config_mutex_);
        return config_;
    }

    void on_receive(endstone::PacketReceiveEvent &event) {
        const auto config = config_snapshot();
        if (config.drop_receive_ids.contains(event.getPacketId())) {
            event.setCancelled(true);
            enqueue_packet("client_to_server", event.getPacketId(), event.getPayload(), event.getPlayer(),
                           event.getAddress(), event.getSubClientId(), "drop-rule", config);
            return;
        }
        enqueue_packet("client_to_server", event.getPacketId(), event.getPayload(), event.getPlayer(),
                       event.getAddress(), event.getSubClientId(), "forward", config);
    }

    void on_send(endstone::PacketSendEvent &event) {
        const auto config = config_snapshot();
        if (config.drop_send_ids.contains(event.getPacketId())) {
            event.setCancelled(true);
            enqueue_packet("server_to_client", event.getPacketId(), event.getPayload(), event.getPlayer(),
                           event.getAddress(), event.getSubClientId(), "drop-rule", config);
            return;
        }
        enqueue_packet("server_to_client", event.getPacketId(), event.getPayload(), event.getPlayer(),
                       event.getAddress(), event.getSubClientId(), "forward", config);
    }

    void enqueue_packet(const std::string &direction,
                        int packet_id,
                        std::string_view payload,
                        endstone::Player *player,
                        const endstone::SocketAddress &address,
                        int sub_client_id,
                        const std::string &action,
                        const CompanionConfig &config) {
        if (config.capture_mode == "off") return;

        // PlayerAuthInput is extremely high-volume. The default packet docs for
        // r26_u3 identify it as 144; sample it unless explicitly selected.
        if (packet_id == 144 && !config.selected_packet_ids.contains(packet_id)) {
            const auto sample = movement_counter_.fetch_add(1) + 1;
            if (sample % config.movement_sample_rate != 0) return;
        }

        const bool redacted = config.redact_packet_ids.contains(packet_id);
        bool include_payload = false;
        if (!redacted && config.capture_mode == "all") include_payload = true;
        if (!redacted && config.capture_mode == "selected" && config.selected_packet_ids.contains(packet_id)) include_payload = true;

        std::string player_name;
        std::string xuid;
        if (player != nullptr) {
            player_name = player->getName();
            xuid = player->getXuid();
        }

        std::ostringstream json;
        json << "{\"type\":\"packet\",\"timestamp\":" << epoch_ms()
             << ",\"direction\":\"" << direction << "\""
             << ",\"packetId\":" << packet_id
             << ",\"payloadBytes\":" << payload.size()
             << ",\"playerName\":\"" << json_escape(player_name) << "\""
             << ",\"xuid\":\"" << json_escape(xuid) << "\""
             << ",\"client\":\"" << json_escape(address.getHostname()) << ':' << address.getPort() << "\""
             << ",\"subClientId\":" << sub_client_id
             << ",\"action\":\"" << action << "\""
             << ",\"redacted\":" << (redacted ? "true" : "false");
        if (include_payload) {
            const auto limit = std::min(config.payload_limit, payload.size());
            json << ",\"hex\":\"" << hex_preview(payload, limit) << "\""
                 << ",\"payloadTruncated\":" << (payload.size() > limit ? "true" : "false");
        }
        json << '}';
        enqueue({json.str()}, config.queue_capacity);
    }

    void capture_metrics() {
        const auto config = config_snapshot();
        auto &server = getServer();
        const auto now = Clock::now();
        const auto ticks = process_cpu_ticks();
        const auto elapsed = std::chrono::duration<double>(now - last_metrics_at_).count();
        const auto tick_rate = static_cast<double>(std::max<long>(1, sysconf(_SC_CLK_TCK)));
        double process_cpu_percent = 0.0;
        if (elapsed > 0.0 && ticks >= last_process_ticks_) {
            process_cpu_percent = ((static_cast<double>(ticks - last_process_ticks_) / tick_rate) / elapsed) * 100.0;
        }
        last_process_ticks_ = ticks;
        last_metrics_at_ = now;
        const auto [network_rx, network_tx] = container_network_bytes();
        const auto online_players = server.getOnlinePlayers();

        std::unordered_map<std::string, std::string> current_presence;
        for (const auto *player : online_players) {
            if (player == nullptr) continue;
            const auto xuid = player->getXuid();
            const auto key = xuid.empty() ? player->getName() : xuid;
            current_presence[key] = player->getName();
            if (!previous_presence_.contains(key)) {
                enqueue_event("presence.join",
                              "Player arrived on " + config.server_id + ": " + player->getName(),
                              "info", player->getName(), xuid);
            }
        }
        for (const auto &[key, player_name] : previous_presence_) {
            if (!current_presence.contains(key)) {
                const auto xuid = key == player_name ? std::string{} : key;
                enqueue_event("presence.leave",
                              "Player left " + config.server_id + ": " + player_name,
                              "info", player_name, xuid);
            }
        }
        previous_presence_ = std::move(current_presence);

        std::ostringstream json;
        json << "{\"type\":\"metrics\",\"timestamp\":" << epoch_ms()
             << ",\"companionVersion\":\"3.6.1\""
             << ",\"capabilitySchema\":1"
             << ",\"serverId\":\"" << json_escape(config.server_id) << "\""
             << ",\"onlineMode\":" << (server.getOnlineMode() ? "true" : "false")
             << ",\"capabilities\":[\"packet_receive\",\"packet_send\",\"metrics\",\"presence\",\"presence_events\",\"transfer\",\"transfer_transactions\",\"arrival_confirmation\",\"drop_rules\",\"capability_negotiation\"]"
             << ",\"currentTps\":" << server.getCurrentTicksPerSecond()
             << ",\"averageTps\":" << server.getAverageTicksPerSecond()
             << ",\"currentMspt\":" << server.getCurrentMillisecondsPerTick()
             << ",\"averageMspt\":" << server.getAverageMillisecondsPerTick()
             << ",\"tickUsage\":" << server.getCurrentTickUsage()
             << ",\"averageTickUsage\":" << server.getAverageTickUsage()
             << ",\"onlinePlayers\":" << online_players.size()
             << ",\"maxPlayers\":" << server.getMaxPlayers()
             << ",\"protocolVersion\":" << server.getProtocolVersion()
             << ",\"minecraftVersion\":\"" << json_escape(server.getMinecraftVersion()) << "\""
             << ",\"processCpuPercent\":" << process_cpu_percent
             << ",\"processMemoryBytes\":" << process_rss_bytes()
             << ",\"containerMemoryBytes\":" << read_uint_file("/sys/fs/cgroup/memory.current")
             << ",\"containerMemoryLimitBytes\":" << read_uint_file("/sys/fs/cgroup/memory.max")
             << ",\"networkRxBytes\":" << network_rx
             << ",\"networkTxBytes\":" << network_tx
             << ",\"queueDepth\":" << queue_size()
             << ",\"queueDrops\":" << dropped_.load()
             << ",\"uploadedRecords\":" << uploaded_.load()
             << ",\"uploadFailures\":" << failures_.load();
        if (config.presence_enabled) {
            json << ",\"players\":[";
            bool first_player = true;
            for (const auto *player : online_players) {
                if (player == nullptr) continue;
                if (!first_player) json << ',';
                first_player = false;
                json << "{\"playerName\":\"" << json_escape(player->getName()) << "\""
                     << ",\"xuid\":\"" << json_escape(player->getXuid()) << "\"";
                if (config.presence_include_address) {
                    json << ",\"address\":\""
                         << json_escape(player->getAddress().getHostname()) << "\"";
                }
                json << '}';
            }
            json << ']';
        }
        json << '}';
        enqueue({json.str()}, config.queue_capacity);
    }

    void enqueue_event(const std::string &type,
                       const std::string &message,
                       const std::string &severity,
                       const std::string &player = {},
                       const std::string &xuid = {}) {
        const auto config = config_snapshot();
        std::ostringstream json;
        json << "{\"type\":\"event\",\"timestamp\":" << epoch_ms()
             << ",\"eventType\":\"" << json_escape(type) << "\""
             << ",\"severity\":\"" << json_escape(severity) << "\""
             << ",\"message\":\"" << json_escape(message) << "\""
             << ",\"player\":\"" << json_escape(player) << "\""
             << ",\"xuid\":\"" << json_escape(xuid) << "\"}";
        enqueue({json.str()}, config.queue_capacity);
    }

    void enqueue(Record record, std::size_t capacity) {
        {
            std::scoped_lock lock(queue_mutex_);
            if (queue_.size() >= capacity) {
                dropped_.fetch_add(1);
                return;
            }
            queue_.push_back(std::move(record));
        }
        queue_cv_.notify_one();
    }

    std::size_t queue_size() const {
        std::scoped_lock lock(queue_mutex_);
        return queue_.size();
    }

    std::string last_upload_error() const {
        std::scoped_lock lock(upload_error_mutex_);
        return last_upload_error_;
    }

    void record_upload_success(std::size_t count) {
        uploaded_.fetch_add(count);
        const auto now = epoch_ms();
        last_upload_attempt_.store(now);
        last_upload_ok_.store(now);
        bool recovered = false;
        {
            std::scoped_lock lock(upload_error_mutex_);
            recovered = !last_upload_error_.empty();
            last_upload_error_.clear();
        }
        if (recovered) {
            getLogger().info("Companion connection restored to {}:{} for server {}.",
                             config_snapshot().dashboard_host, config_snapshot().dashboard_port,
                             config_snapshot().server_id);
        }
    }

    void record_upload_failure(const std::string &error) {
        failures_.fetch_add(1);
        const auto now = epoch_ms();
        last_upload_attempt_.store(now);
        {
            std::scoped_lock lock(upload_error_mutex_);
            last_upload_error_ = error.empty() ? "Unknown dashboard connection failure" : error;
        }
        const auto previous_warning = last_warning_at_.load();
        if (previous_warning == 0 || now - previous_warning >= 30000) {
            last_warning_at_.store(now);
            const auto config = config_snapshot();
            getLogger().warning("Companion upload failed to {}:{} server={} secretFingerprint={}: {}",
                                config.dashboard_host, config.dashboard_port, config.server_id,
                                secret_fingerprint(config.shared_secret), last_upload_error());
        }
    }

    void upload_loop() {
        while (running_.load() || queue_size() > 0) {
            const auto config = config_snapshot();
            std::vector<Record> batch;
            {
                std::unique_lock lock(queue_mutex_);
                queue_cv_.wait_for(lock, std::chrono::milliseconds(config.flush_ms), [this] {
                    return !queue_.empty() || !running_.load();
                });
                const auto count = std::min(config.batch_size, queue_.size());
                batch.reserve(count);
                for (std::size_t i = 0; i < count; ++i) {
                    batch.push_back(std::move(queue_.front()));
                    queue_.pop_front();
                }
            }
            if (batch.empty()) continue;

            std::ostringstream body;
            body << "{\"serverId\":\"" << json_escape(config.server_id) << "\",\"records\":[";
            for (std::size_t i = 0; i < batch.size(); ++i) {
                if (i != 0) body << ',';
                body << batch[i].json;
            }
            body << "]}";

            std::string error;
            if (HttpClient::post(config, body.str(), error)) {
                record_upload_success(batch.size());
                continue;
            }

            record_upload_failure(error);
            if (!running_.load()) {
                // Never hold BDS shutdown hostage to an unavailable dashboard.
                dropped_.fetch_add(batch.size());
                continue;
            }

            // Put failed records back at the front while honoring the current
            // bounded queue. Packet tracking degrades gracefully instead of
            // blocking the BDS tick thread when the dashboard is unavailable.
            {
                std::scoped_lock lock(queue_mutex_);
                for (auto iterator = batch.rbegin(); iterator != batch.rend(); ++iterator) {
                    if (queue_.size() >= config.queue_capacity) {
                        dropped_.fetch_add(1);
                        break;
                    }
                    queue_.push_front(std::move(*iterator));
                }
            }
            const auto retry_until = Clock::now() +
                std::chrono::seconds(std::max(1, config.reconnect_seconds));
            while (running_.load() && Clock::now() < retry_until) {
                std::this_thread::sleep_for(std::chrono::milliseconds(100));
            }
        }
    }

    fs::path config_path_;
    mutable std::mutex config_mutex_;
    CompanionConfig config_;

    mutable std::mutex queue_mutex_;
    std::condition_variable queue_cv_;
    std::deque<Record> queue_;
    std::thread worker_;
    std::atomic<bool> running_{false};
    std::atomic<std::uint64_t> dropped_{0};
    std::atomic<std::uint64_t> uploaded_{0};
    std::atomic<std::uint64_t> failures_{0};
    std::atomic<std::uint64_t> movement_counter_{0};
    std::atomic<std::uint64_t> last_upload_attempt_{0};
    std::atomic<std::uint64_t> last_upload_ok_{0};
    std::atomic<std::uint64_t> last_warning_at_{0};
    mutable std::mutex upload_error_mutex_;
    std::string last_upload_error_;
    std::unordered_map<std::string, std::string> previous_presence_;
    std::uint64_t last_process_ticks_{0};
    Clock::time_point last_metrics_at_{Clock::now()};
};

ENDSTONE_PLUGIN("ninjos_proxie_companion", "3.6.1", NinjOSProxieCompanion) {
    prefix = "Ninj-OS Proxie";
    description = "Identity, permission, packet, and performance bridge for Ninj-OS transparent and full-proxy modes";
    authors = {"Ninj-OS"};
    command("ninjosproxiemonitor")
        .description("View or reload the Ninj-OS Proxie companion")
        .usages("/ninjosproxiemonitor [status|reload|probe]")
        .aliases("npm")
        .permissions("ninjos.proxie.monitor");
    command("ninjosserver")
        .description("Transfer through a protected Ninj-OS Proxie route")
        .usages("/ninjosserver <backend>")
        .aliases("nserver")
        .permissions("ninjos.proxie.transfer");
    permission("ninjos.proxie.monitor")
        .description("Allows management of the Ninj-OS Proxie companion")
        .default_(endstone::PermissionDefault::Operator);
    permission("ninjos.proxie.transfer")
        .description("Allows players to transfer through Ninj-OS Proxie")
        .default_(endstone::PermissionDefault::True);
}
