import AppKit
import Foundation
import ServiceManagement

struct Tokens: Decodable {
    var input: Int64 = 0
    var output: Int64 = 0
    var cacheRead: Int64 = 0
    var cacheWrite: Int64 = 0
    var total: Int64 { input + output + cacheRead + cacheWrite }
}

struct Stats: Decodable {
    struct Config: Decodable { let inject: Bool; let autosave: Bool }
    struct Day: Decodable { let date: String; let total: Int64; let clients: [String: Int64] }
    struct Session: Decodable { let client: String; let project: String; let dir: String?; let updated: String }
    struct Activity: Decodable {
        let today: [String: Tokens]
        let todayTotal: Int64
        let days: [Day]
        let active: [Session]?
    }
    struct Usage: Decodable { let target: String; let path: String?; let used: Int; let limit: Int; let error: String? }
    struct Recent: Decodable { let ts: String; let agent: String?; let project: String?; let text: String }
    struct Client: Decodable { let name: String; let detected: Bool; let configured: Bool }

    let version: String
    let dataDir: String
    let config: Config
    let activity: Activity
    let today: [String: Int]
    let memory: [Usage]
    let notes: Int
    let logs: Int
    let recent: [Recent]?
    let clients: [Client]
}

enum Core {
    static let candidates: [String] = {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        var paths = [ProcessInfo.processInfo.environment["AIBASE_BIN"] ?? "", UserDefaults.standard.string(forKey: "coreBinary") ?? ""]
        if let res = Bundle.main.resourceURL?.appendingPathComponent("aibase").path { paths.append(res) }
        paths += ["\(home)/.local/bin/aibase", "/opt/homebrew/bin/aibase", "/usr/local/bin/aibase", "\(home)/go/bin/aibase"]
        return paths.filter { !$0.isEmpty }
    }()

    static var path: String? { candidates.first { FileManager.default.isExecutableFile(atPath: $0) } }

    struct Failure: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }

    static func run(_ args: [String], timeout: TimeInterval = 20) throws -> Data {
        guard let exe = path else { throw Failure(message: L("找不到 aibase，请先安装：curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | sh", "aibase not found. Install it first: curl -fsSL https://raw.githubusercontent.com/xiaoxin031/aibase/main/install.sh | sh")) }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: exe)
        p.arguments = args
        p.currentDirectoryURL = FileManager.default.homeDirectoryForCurrentUser
        let out = Pipe(), err = Pipe()
        p.standardOutput = out
        p.standardError = err
        p.standardInput = FileHandle.nullDevice
        try p.run()
        let timer = DispatchWorkItem { if p.isRunning { p.terminate() } }
        DispatchQueue.global().asyncAfter(deadline: .now() + timeout, execute: timer)
        let data = out.fileHandleForReading.readDataToEndOfFile()
        let errData = err.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        timer.cancel()
        if p.terminationStatus != 0 {
            let msg = String(data: errData.isEmpty ? data : errData, encoding: .utf8) ?? ""
            throw Failure(message: msg.trimmingCharacters(in: .whitespacesAndNewlines))
        }
        return data
    }
}

@MainActor
final class Store: ObservableObject {
    @Published var stats: Stats?
    @Published var error: String?
    @Published var loading = false
    @Published var updated: Date?
    @Published var results: [Hit] = []
    @Published var searching = false
    @Published var showCount = UserDefaults.standard.object(forKey: "showCount") as? Bool ?? true {
        didSet { UserDefaults.standard.set(showCount, forKey: "showCount") }
    }
    @Published var launchAtLogin = SMAppService.mainApp.status == .enabled

    private var timer: Timer?
    private var searchTask: Task<Void, Never>?

    init() {
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
    }

    var activeCount: Int { stats?.activity.active?.count ?? 0 }

    func refresh() {
        guard !loading else { return }
        loading = true
        Task.detached {
            let result = Result { try Core.run(["stats", "--json"]) }
            await MainActor.run {
                self.loading = false
                switch result {
                case .success(let data):
                    do {
                        let d = JSONDecoder()
                        d.keyDecodingStrategy = .convertFromSnakeCase
                        self.stats = try d.decode(Stats.self, from: data)
                        self.updated = Date()
                        self.error = nil
                    } catch {
                        self.error = L("无法解析 aibase 输出，请更新 aibase", "Unexpected aibase output; update aibase")
                    }
                case .failure(let e):
                    self.error = e.localizedDescription.contains("unknown command")
                        ? L("aibase 版本过旧，请重新运行安装命令更新", "aibase is too old for this app; rerun the installer to update")
                        : e.localizedDescription
                }
            }
        }
    }

    func set(_ key: String, _ value: Bool) {
        Task.detached {
            _ = try? Core.run(["config", key, value ? "true" : "false", "--json"])
            await MainActor.run { self.refresh() }
        }
    }

    func search(_ query: String) {
        searchTask?.cancel()
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { results = []; searching = false; return }
        searching = true
        searchTask = Task {
            try? await Task.sleep(nanoseconds: 250_000_000)
            if Task.isCancelled { return }
            let text = await Task.detached { (try? Core.run(["search", q])).flatMap { String(data: $0, encoding: .utf8) } ?? "" }.value
            if Task.isCancelled { return }
            self.results = Hit.parse(text)
            self.searching = false
        }
    }

    func doctor() {
        Task.detached {
            let data = (try? Core.run(["doctor"], timeout: 60)) ?? Data()
            let url = FileManager.default.temporaryDirectory.appendingPathComponent("aibase-doctor.txt")
            try? data.write(to: url)
            await MainActor.run { _ = NSWorkspace.shared.open(url) }
        }
    }

    func openDataDir() {
        if let dir = stats?.dataDir { NSWorkspace.shared.open(URL(fileURLWithPath: dir)) }
    }

    // The app starts at login unless the user turned that off once.
    func offerLaunchAtLogin() {
        guard !UserDefaults.standard.bool(forKey: "loginItemOffered") else { return }
        UserDefaults.standard.set(true, forKey: "loginItemOffered")
        setLaunchAtLogin(true)
    }

    func setLaunchAtLogin(_ on: Bool) {
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
        } catch {}
        launchAtLogin = SMAppService.mainApp.status == .enabled
    }
}

struct Hit: Identifiable {
    let id = UUID()
    let note: Bool
    let title: String
    var detail = ""
    var agent = ""
    var body = ""

    // Search output: a count line, then "[note] slug — desc" / "[log date path · agent]"
    // headers, each followed by a snippet.
    static func parse(_ text: String) -> [Hit] {
        var hits: [Hit] = []
        for line in text.components(separatedBy: "\n") {
            if line.hasPrefix("[note] ") {
                let rest = line.dropFirst(7)
                let parts = rest.components(separatedBy: " — ")
                hits.append(Hit(note: true, title: parts.count > 1 ? parts.dropFirst().joined(separator: " — ") : String(rest), detail: parts[0]))
            } else if line.hasPrefix("[log "), line.hasSuffix("]") {
                let head = line.dropFirst(5).dropLast()
                let fields = head.components(separatedBy: " · ")
                let date = String(fields[0].prefix(10))
                let path = fields[0].dropFirst(min(11, fields[0].count))
                hits.append(Hit(note: false, title: path.isEmpty ? L("会话日志", "Session log") : URL(fileURLWithPath: String(path)).lastPathComponent,
                                detail: date, agent: fields.count > 1 ? fields[1].replacingOccurrences(of: "legacy-", with: "") : ""))
            } else if !hits.isEmpty {
                let t = line.replacingOccurrences(of: #"\*\*|`|^#+ "#, with: "", options: .regularExpression).trimmingCharacters(in: .whitespaces)
                if !t.isEmpty { hits[hits.count - 1].body += (hits[hits.count - 1].body.isEmpty ? "" : " ") + t }
            }
        }
        return hits
    }
}

let isChinese = Locale.preferredLanguages.first?.hasPrefix("zh") ?? false
func L(_ zh: String, _ en: String) -> String { isChinese ? zh : en }

func compact(_ n: Int64) -> String {
    let v = Double(n)
    switch n {
    case 1_000_000_000...: return String(format: "%.2fB", v / 1e9)
    case 1_000_000...: return String(format: "%.1fM", v / 1e6)
    case 1_000...: return String(format: "%.1fK", v / 1e3)
    default: return "\(n)"
    }
}

func parseDate(_ s: String) -> Date? {
    // Go emits up to nine fractional digits; ISO8601DateFormatter takes three.
    var t = s
    if let dot = t.firstIndex(of: "."), let end = t[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" }) {
        let frac = t[t.index(after: dot)..<end].prefix(3)
        t = String(t[..<dot]) + "." + frac.padding(toLength: 3, withPad: "0", startingAt: 0) + String(t[end...])
    }
    let f = ISO8601DateFormatter()
    f.formatOptions = t.contains(".") ? [.withInternetDateTime, .withFractionalSeconds] : [.withInternetDateTime]
    return f.date(from: t)
}

func relative(_ s: String) -> String {
    parseDate(s).map(relative) ?? ""
}

func relative(_ d: Date) -> String {
    if Date().timeIntervalSince(d) < 60 { return L("刚刚", "just now") }
    let f = RelativeDateTimeFormatter()
    f.unitsStyle = .short
    f.locale = Locale(identifier: isChinese ? "zh_CN" : "en_US")
    return f.localizedString(for: d, relativeTo: Date())
}
