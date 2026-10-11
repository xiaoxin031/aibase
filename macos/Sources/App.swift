import AppKit
import SwiftUI

@main
struct Main {
    static func main() {
        let args = CommandLine.arguments
        if args.count == 3, args[1] == "--render-iconset" {
            do { try Mark.renderIconset(to: args[2]); exit(0) } catch { print(error); exit(1) }
        }
        if args.count == 2, args[1] == "--check" {
            do {
                let d = JSONDecoder()
                d.keyDecodingStrategy = .convertFromSnakeCase
                let s = try d.decode(Stats.self, from: Core.run(["stats", "--json"]))
                print("ok: aibase \(s.version), \(s.activity.days.count) days, \(s.memory.count) memory stores")
                exit(0)
            } catch {
                print("error: \(error)")
                exit(1)
            }
        }
        if args.count >= 3, args[1] == "--snapshot" {
            snapshot(to: args[2], dark: args.count > 3 && args[3] == "dark")
        }
        AibaseBar.main()
    }
}

// Renders the dashboard offscreen to a PNG (for docs and visual checks).
@MainActor
func snapshot(to path: String, dark: Bool) {
    let app = NSApplication.shared
    app.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
    let store = Store()
    RunLoop.main.run(until: Date().addingTimeInterval(3))
    let view = NSHostingView(rootView: Dashboard().environmentObject(store).background(Color(nsColor: .windowBackgroundColor)))
    view.appearance = app.appearance
    view.frame.size = view.fittingSize
    let window = NSWindow(contentRect: view.frame, styleMask: .borderless, backing: .buffered, defer: false)
    window.contentView = view
    view.layoutSubtreeIfNeeded()
    RunLoop.main.run(until: Date().addingTimeInterval(0.5))
    guard let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) else { exit(1) }
    view.cacheDisplay(in: view.bounds, to: rep)
    try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: path))
    exit(0)
}

struct AibaseBar: App {
    @StateObject private var store: Store

    init() {
        let store = Store()
        store.offerLaunchAtLogin()
        _store = StateObject(wrappedValue: store)
    }

    var body: some Scene {
        MenuBarExtra {
            Dashboard().environmentObject(store)
        } label: {
            Image(nsImage: Mark.template)
            if store.showCount && store.activeCount > 0 {
                Text("\(store.activeCount)")
            }
        }
        .menuBarExtraStyle(.window)
    }
}

struct Dashboard: View {
    @EnvironmentObject var store: Store
    @State private var query = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Header()
            if let s = store.stats {
                SearchField(query: $query).padding(.top, 12).padding(.bottom, 4)
                if query.isEmpty {
                    TokensSection(a: s.activity)
                    SessionsSection(sessions: s.activity.active ?? [])
                    MemorySection(s: s)
                    if let recent = s.recent, !recent.isEmpty { RecentSection(recent: Array(recent.prefix(2))) }
                } else {
                    SearchResults().padding(.top, 8)
                }
                Footer()
            } else if let e = store.error {
                Problem(message: e)
            } else {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, minHeight: 120)
            }
        }
        .padding(.horizontal, 16).padding(.vertical, 14)
        .frame(width: 340)
        .onAppear { store.refresh() }
        .onChange(of: query) { store.search($0) }
    }
}

enum Fonts {
    static let hero = Font.system(size: 30, weight: .semibold, design: .rounded)
    static let title = Font.system(size: 14, weight: .semibold)
    static let body = Font.system(size: 12)
    static let label = Font.system(size: 12, weight: .medium)
    static let caption = Font.system(size: 11)
    static let heading = Font.system(size: 11, weight: .semibold)
}

struct Block<Content: View>: View {
    let title: String
    var trailing: String? = nil
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .firstTextBaseline) {
                Text(title).font(Fonts.heading).foregroundStyle(.secondary)
                Spacer()
                if let t = trailing { Text(t).font(Fonts.caption).monospacedDigit().foregroundStyle(.tertiary) }
            }
            content
        }
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) { Divider() }
    }
}

// Clickable rows get a soft highlight that bleeds slightly past the text column.
struct HoverRow: ViewModifier {
    @State private var hover = false

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 6).padding(.vertical, 3)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(Color.primary.opacity(hover ? 0.07 : 0)))
            .padding(.horizontal, -6)
            .contentShape(Rectangle())
            .onHover { hover = $0 }
            .animation(.easeOut(duration: 0.12), value: hover)
    }
}

extension View {
    func hoverRow() -> some View { modifier(HoverRow()) }

    @ViewBuilder func numeric() -> some View {
        if #available(macOS 14, *) { contentTransition(.numericText()) } else { self }
    }
}

struct Dot: View {
    let client: String
    var body: some View { Circle().fill(Color(Palette.client(client))).frame(width: 7, height: 7) }
}

struct Header: View {
    @EnvironmentObject var store: Store

    var body: some View {
        HStack(spacing: 10) {
            Image(nsImage: Mark.appIcon(size: 64)).resizable().frame(width: 30, height: 30).padding(-2)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Text("aibase").font(Fonts.title)
                    if let v = store.stats?.version { Text(v).font(Fonts.caption).foregroundStyle(.tertiary) }
                }
                if let s = store.stats {
                    let (ok, text) = status(s)
                    HStack(spacing: 5) {
                        Circle().fill(ok ? Color.green : Color.orange).frame(width: 6, height: 6)
                        Text(text).lineLimit(1)
                    }
                    .font(Fonts.caption).foregroundStyle(.secondary)
                }
            }
            Spacer()
            Button { store.refresh() } label: {
                if store.loading { ProgressView().controlSize(.mini) } else { Image(systemName: "arrow.clockwise") }
            }
            .buttonStyle(IconButton()).keyboardShortcut("r").help(L("刷新 (⌘R)", "Refresh (⌘R)"))
            SettingsMenu()
        }
    }

    private func status(_ s: Stats) -> (Bool, String) {
        let wired = s.clients.filter(\.configured).count
        if wired == 0 { return (false, L("还没有接入 AI 客户端", "No AI client connected")) }
        if !s.config.inject { return (false, L("记忆注入已暂停", "Memory injection paused")) }
        if !s.config.autosave { return (false, L("自动保存已关闭", "Auto-save is off")) }
        return (true, L("记忆自动运行 · \(wired) 个客户端", "Memory on · \(wired) clients"))
    }
}

struct IconButton: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: .medium))
            .foregroundStyle(.secondary)
            .frame(width: 24, height: 24)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(Color.primary.opacity(configuration.isPressed ? 0.12 : 0)))
            .contentShape(Rectangle())
    }
}

struct SettingsMenu: View {
    @EnvironmentObject var store: Store

    var body: some View {
        Menu {
            if let c = store.stats?.config {
                Toggle(L("新会话自动带上记忆", "Load memory into new sessions"), isOn: Binding(get: { c.inject }, set: { store.set("inject", $0) }))
                Toggle(L("会话结束自动保存", "Auto-save after sessions"), isOn: Binding(get: { c.autosave }, set: { store.set("autosave", $0) }))
                Divider()
            }
            Toggle(L("登录时启动", "Launch at login"), isOn: Binding(get: { store.launchAtLogin }, set: { store.setLaunchAtLogin($0) }))
            Toggle(L("菜单栏显示活跃会话数", "Show active sessions in menu bar"), isOn: $store.showCount)
            Divider()
            Button(L("打开数据目录", "Open data folder")) { store.openDataDir() }
            Button(L("运行体检", "Run doctor")) { store.doctor() }
            Button(L("项目主页", "Project page")) { reveal(URL(string: "https://github.com/xiaoxin031/aibase")!) }
            Divider()
            Button(L("退出菜单栏（记忆照常运行）", "Quit menu bar app (memory keeps working)")) { NSApp.terminate(nil) }
        } label: {
            Image(systemName: "ellipsis")
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .frame(width: 24, height: 24)
    }
}

struct TokensSection: View {
    let a: Stats.Activity
    @State private var hover: Int?

    private var clients: [(String, Tokens)] { a.today.filter { $0.value.total > 0 }.sorted { $0.value.total > $1.value.total } }

    var body: some View {
        Block(title: L("今日 Token", "Tokens today"), trailing: trailing) {
            HStack(alignment: .bottom) {
                Text(compact(a.todayTotal))
                    .font(Fonts.hero)
                    .monospacedDigit()
                    .numeric()
                    .animation(.easeOut(duration: 0.3), value: a.todayTotal)
                Spacer()
                WeekBars(days: a.days, hover: $hover)
            }
            metrics
            if a.todayTotal > 0 {
                GeometryReader { g in
                    HStack(spacing: 2) {
                        ForEach(clients, id: \.0) { name, t in
                            Rectangle().fill(Color(Palette.client(name)))
                                .frame(width: max(2, (g.size.width - CGFloat(clients.count - 1) * 2) * CGFloat(t.total) / CGFloat(a.todayTotal)))
                        }
                    }
                    .clipShape(Capsule())
                }
                .frame(height: 4)
                VStack(spacing: 5) {
                    ForEach(clients, id: \.0) { name, t in
                        HStack(spacing: 7) {
                            Dot(client: name)
                            Text(Palette.clientName(name)).font(Fonts.label)
                            Spacer()
                            Text(compact(t.total)).font(Fonts.body).monospacedDigit()
                            Text(percent(t.total, a.todayTotal)).font(Fonts.caption).monospacedDigit().foregroundStyle(.tertiary).frame(width: 34, alignment: .trailing)
                        }
                    }
                }
            }
        }
    }

    // Hovering a bar shows that day; otherwise the 7-day daily average.
    private var trailing: String {
        if let i = hover, a.days.indices.contains(i) {
            let d = a.days[i]
            return "\(i == a.days.count - 1 ? L("今天", "Today") : String(d.date.dropFirst(5))) · \(compact(d.total))"
        }
        return L("近 7 天 ", "7 days ") + compact(a.days.map(\.total).reduce(0, +))
    }

    private var metrics: some View {
        var t = Tokens()
        for (_, v) in a.today {
            t.input += v.input; t.output += v.output; t.cacheRead += v.cacheRead; t.cacheWrite += v.cacheWrite
        }
        return HStack(spacing: 16) {
            Metric(label: L("输入", "In"), value: compact(t.input))
            Metric(label: L("输出", "Out"), value: compact(t.output))
            Metric(label: L("缓存命中", "Cache hit"), value: percent(t.cacheRead, t.input + t.cacheRead + t.cacheWrite))
        }
    }
}

struct Metric: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 4) {
            Text(label).font(Fonts.caption).foregroundStyle(.secondary)
            Text(value).font(Fonts.label).monospacedDigit()
        }
    }
}

func percent(_ part: Int64, _ whole: Int64) -> String {
    guard whole > 0 else { return "0%" }
    let p = Double(part) / Double(whole) * 100
    return p > 0 && p < 1 ? "<1%" : "\(Int(p.rounded()))%"
}

struct WeekBars: View {
    let days: [Stats.Day]
    @Binding var hover: Int?

    private static let weekday: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: isChinese ? "zh_CN" : "en_US")
        f.dateFormat = "EEEEE"
        return f
    }()

    private static let iso: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()

    var body: some View {
        let peak = max(days.map(\.total).max() ?? 1, 1)
        HStack(alignment: .bottom, spacing: 5) {
            ForEach(Array(days.enumerated()), id: \.offset) { i, d in
                let today = i == days.count - 1
                VStack(spacing: 4) {
                    RoundedRectangle(cornerRadius: 2, style: .continuous)
                        .fill(today ? Color(Palette.violet) : Color.primary.opacity(hover == i ? 0.4 : 0.16))
                        .frame(width: 8, height: max(2, 34 * CGFloat(d.total) / CGFloat(peak)))
                    Text(Self.iso.date(from: d.date).map { Self.weekday.string(from: $0) } ?? "")
                        .font(.system(size: 9, weight: today ? .semibold : .regular))
                        .foregroundStyle(today ? AnyShapeStyle(.primary) : AnyShapeStyle(.tertiary))
                }
                .frame(maxHeight: .infinity, alignment: .bottom)
                .contentShape(Rectangle())
                .onHover { if $0 { hover = i } else if hover == i { hover = nil } }
            }
        }
        .frame(height: 50)
        .animation(.easeOut(duration: 0.12), value: hover)
    }
}

struct SessionsSection: View {
    let sessions: [Stats.Session]

    // Several sessions in the same client and folder collapse into one row.
    private struct Group: Identifiable {
        let client: String
        let project: String
        let dir: String?
        let count: Int
        let updated: String
        var id: String { client + "\u{0}" + (dir ?? project) }
    }

    private var groups: [Group] {
        var out: [Group] = []
        for s in sessions {
            let key = s.client + "\u{0}" + (s.dir ?? s.project)
            if let i = out.firstIndex(where: { $0.id == key }) {
                let g = out[i]
                out[i] = Group(client: g.client, project: g.project, dir: g.dir, count: g.count + 1, updated: g.updated)
            } else {
                out.append(Group(client: s.client, project: s.project, dir: s.dir, count: 1, updated: s.updated))
            }
        }
        return out
    }

    var body: some View {
        Block(title: L("活跃会话", "Active sessions"), trailing: L("近 15 分钟", "last 15 min")) {
            if sessions.isEmpty {
                Text(L("暂无进行中的 AI 会话", "No AI sessions right now")).font(Fonts.body).foregroundStyle(.tertiary)
            } else {
                let g = groups
                VStack(spacing: 1) {
                    ForEach(g.prefix(5)) { row($0) }
                    if g.count > 5 {
                        Text(L("还有 \(g.count - 5) 个", "\(g.count - 5) more")).font(Fonts.caption).foregroundStyle(.tertiary).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
    }

    private func row(_ g: Group) -> some View {
        Button {
            if let d = g.dir, !d.isEmpty { reveal(URL(fileURLWithPath: d)) }
        } label: {
            HStack(spacing: 7) {
                Live(color: Color(Palette.client(g.client)), on: (parseDate(g.updated).map { Date().timeIntervalSince($0) < 120 }) ?? false)
                Text(name(g.project)).font(Fonts.label).lineLimit(1).truncationMode(.tail)
                Text(Palette.clientName(g.client) + (g.count > 1 ? " ×\(g.count)" : "")).font(Fonts.caption).monospacedDigit().foregroundStyle(.secondary).fixedSize()
                Spacer(minLength: 8)
                Text(relative(g.updated)).font(Fonts.caption).foregroundStyle(.tertiary).fixedSize()
            }
            .hoverRow()
        }
        .buttonStyle(.plain)
        .help(g.dir ?? "")
    }

    private func name(_ project: String) -> String {
        switch project {
        case "": return "—"
        case "~": return L("主目录", "Home")
        default: return project
        }
    }
}

// Static on purpose: a looping animation keeps redrawing even while the panel is closed.
struct Live: View {
    let color: Color
    let on: Bool

    var body: some View {
        ZStack {
            if on { Circle().fill(color.opacity(0.25)).frame(width: 13, height: 13) }
            Circle().fill(color).frame(width: 7, height: 7)
        }
        .frame(width: 13, height: 13)
    }
}

struct MemorySection: View {
    let s: Stats

    var body: some View {
        Block(title: L("记忆", "Memory"), trailing: L("今日保存 \(s.today["saves"] ?? 0) 次", "\(s.today["saves"] ?? 0) saves today")) {
            VStack(spacing: 1) {
                ForEach(s.memory, id: \.target) { u in
                    Button { if let p = u.path { reveal(URL(fileURLWithPath: p)) } } label: { row(u) }
                        .buttonStyle(.plain)
                        .help(u.path ?? "")
                }
            }
            HStack(spacing: 14) {
                count(s.notes, L("笔记", "notes"))
                count(s.logs, L("会话日志", "session logs"))
                count(s.today["reviews"] ?? 0, L("今日复盘", "reviews today"))
            }
        }
    }

    private func count(_ n: Int, _ label: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 4) {
            Text("\(n)").font(Fonts.label).monospacedDigit().numeric().animation(.easeOut(duration: 0.3), value: n)
            Text(label).font(Fonts.caption).foregroundStyle(.secondary)
        }
    }

    private func row(_ u: Stats.Usage) -> some View {
        let ratio = u.limit > 0 ? min(1, CGFloat(u.used) / CGFloat(u.limit)) : 0
        let full = ratio > 0.9
        return HStack(spacing: 10) {
            Text(label(u.target)).font(Fonts.body).frame(width: 44, alignment: .leading)
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.primary.opacity(0.08))
                    Capsule().fill(full ? Color.orange : Color(Palette.violet)).frame(width: max(3, g.size.width * ratio))
                }
            }
            .frame(height: 4)
            Text("\(u.used) / \(u.limit)").font(Fonts.caption).monospacedDigit()
                .foregroundStyle(full ? AnyShapeStyle(Color.orange) : AnyShapeStyle(.tertiary))
                .frame(width: 74, alignment: .trailing)
        }
        .hoverRow()
    }

    private func label(_ target: String) -> String {
        switch target {
        case "user": return L("关于你", "You")
        case "memory": return L("全局", "Global")
        default: return L("项目", "Project")
        }
    }
}

struct ClientLabel: View {
    let agent: String

    var body: some View {
        HStack(spacing: 5) {
            Dot(client: agent)
            Text(Palette.clientName(agent)).font(Fonts.label)
        }
    }
}

struct RecentSection: View {
    let recent: [Stats.Recent]
    @State private var open: Int?

    var body: some View {
        Block(title: L("最近会话摘要", "Recent sessions")) {
            VStack(spacing: 4) {
                ForEach(Array(recent.enumerated()), id: \.offset) { i, r in
                    Button { open = open == i ? nil : i } label: {
                        VStack(alignment: .leading, spacing: 3) {
                            HStack(spacing: 7) {
                                let agent = (r.agent ?? "").replacingOccurrences(of: "legacy-", with: "")
                                Dot(client: agent)
                                if let p = r.project, !p.isEmpty {
                                    Text(project(p)).font(Fonts.label).lineLimit(1)
                                    Text(Palette.clientName(agent)).font(Fonts.caption).foregroundStyle(.secondary).fixedSize()
                                } else {
                                    Text(Palette.clientName(agent)).font(Fonts.label)
                                }
                                Spacer(minLength: 8)
                                Text(relative(r.ts)).font(Fonts.caption).foregroundStyle(.tertiary).fixedSize()
                            }
                            Text(summary(r.text, full: open == i)).font(Fonts.body).foregroundStyle(.primary.opacity(0.85)).lineLimit(open == i ? 12 : 2)
                                .lineSpacing(2)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        .hoverRow()
                    }
                    .buttonStyle(.plain)
                }
            }
            .animation(.easeOut(duration: 0.18), value: open)
        }
    }

    // Project keys end in a short hash of the path.
    private func project(_ key: String) -> String {
        guard let r = key.range(of: #"-[0-9a-f]{8}$"#, options: .regularExpression) else { return key }
        return String(key[..<r.lowerBound])
    }

    // Summaries often open with their own date, which the timestamp already shows.
    private func summary(_ text: String, full: Bool) -> String {
        var t = full ? text : (text.split(separator: "\n").first.map(String.init) ?? "")
        if let r = t.range(of: #"^\d{4}-\d{2}-\d{2}[：:]\s*"#, options: .regularExpression) { t.removeSubrange(r) }
        return t
    }
}

struct SearchField: View {
    @Binding var query: String
    @EnvironmentObject var store: Store

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass").foregroundStyle(.tertiary)
            TextField(L("搜索记忆、笔记、历史会话", "Search memory, notes, sessions"), text: $query)
                .textFieldStyle(.plain)
                .onExitCommand { query = "" }
            if store.searching { ProgressView().controlSize(.mini) }
            if !query.isEmpty {
                Button { query = "" } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(.tertiary) }.buttonStyle(.plain)
            }
        }
        .font(Fonts.body)
        .padding(.horizontal, 9).padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(Color.primary.opacity(0.06)))
    }
}

struct SearchResults: View {
    @EnvironmentObject var store: Store
    @State private var open: UUID?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 2) {
                if store.results.isEmpty && !store.searching {
                    Text(L("没有找到相关记忆", "No matches")).font(Fonts.body).foregroundStyle(.tertiary)
                        .frame(maxWidth: .infinity).padding(.top, 40)
                }
                ForEach(store.results) { h in
                    Button { open = open == h.id ? nil : h.id } label: { hit(h) }.buttonStyle(.plain)
                }
            }
        }
        .frame(height: 380)
        .animation(.easeOut(duration: 0.18), value: open)
    }

    private func hit(_ h: Hit) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Image(systemName: h.note ? "doc.text" : "clock").font(.system(size: 10)).foregroundStyle(.secondary).frame(width: 12)
                Text(h.title).font(Fonts.label).lineLimit(1)
                Spacer(minLength: 4)
                if !h.agent.isEmpty { ClientLabel(agent: h.agent) }
                Text(h.detail).font(Fonts.caption).monospacedDigit().foregroundStyle(.tertiary).lineLimit(1)
            }
            Text(h.body).font(Fonts.body).foregroundStyle(.secondary).lineLimit(open == h.id ? nil : 2)
                .lineSpacing(2)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
        }
        .padding(.vertical, 5)
        .hoverRow()
    }
}

struct Footer: View {
    var body: some View {
        HStack(spacing: 5) {
            Image(systemName: "checkmark.shield").font(.system(size: 10))
            Text(L("AI 客户端自动读写记忆，退出本 App 也不受影响", "Memory runs inside your AI clients; quitting is safe"))
                .lineLimit(1).minimumScaleFactor(0.85)
        }
        .font(Fonts.caption).foregroundStyle(.tertiary)
        .padding(.top, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) { Divider() }
    }
}

struct Problem: View {
    let message: String
    @EnvironmentObject var store: Store

    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 20)).foregroundStyle(.orange)
            Text(message).font(Fonts.body).multilineTextAlignment(.center).textSelection(.enabled)
            Button(L("重试", "Retry")) { store.refresh() }
        }
        .padding(.vertical, 20)
        .frame(maxWidth: .infinity)
    }
}
