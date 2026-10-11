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
        VStack(spacing: 8) {
            Header()
            if let s = store.stats {
                if query.isEmpty {
                    TokensCard(a: s.activity)
                    SessionsCard(sessions: s.activity.active ?? [])
                    MemoryCard(s: s)
                    if let recent = s.recent, !recent.isEmpty { RecentCard(recent: Array(recent.prefix(2))) }
                }
                SearchField(query: $query)
                if !query.isEmpty { SearchResults() }
                Footer(s: s)
            } else if let e = store.error {
                Problem(message: e)
            } else {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, minHeight: 120)
            }
        }
        .padding(12)
        .frame(width: 348)
        .onAppear { store.refresh() }
        .onChange(of: query) { store.search($0) }
    }
}

struct Card<Content: View>: View {
    let title: String
    var trailing: String? = nil
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(title).font(.system(size: 11, weight: .semibold)).foregroundStyle(.secondary).textCase(.uppercase)
                Spacer()
                if let t = trailing { Text(t).font(.system(size: 11)).monospacedDigit().foregroundStyle(.secondary) }
            }
            content
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Color.primary.opacity(0.045)))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Color.primary.opacity(0.06)))
    }
}

// Clickable rows get a soft highlight that bleeds slightly past the card's text column.
struct HoverRow: ViewModifier {
    @State private var hover = false

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 6).padding(.vertical, 3)
            .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(Color.primary.opacity(hover ? 0.07 : 0)))
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

struct Header: View {
    @EnvironmentObject var store: Store

    var body: some View {
        HStack(spacing: 10) {
            Image(nsImage: Mark.appIcon(size: 64)).resizable().frame(width: 34, height: 34)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text("aibase").font(.system(size: 15, weight: .bold, design: .rounded))
                    if let v = store.stats?.version { Text(v).font(.system(size: 10, weight: .medium)).foregroundStyle(.secondary) }
                }
                if let c = store.stats?.config {
                    HStack(spacing: 5) {
                        Circle().fill(c.inject && c.autosave ? Color.green : Color.orange).frame(width: 6, height: 6)
                        Text(status(c))
                        if let u = store.updated {
                            Text("·").foregroundStyle(.tertiary)
                            Text(L("更新于 ", "Updated ") + relative(u))
                        }
                    }
                    .font(.system(size: 11)).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            Spacer(minLength: 4)
            Button { store.refresh() } label: {
                Image(systemName: "arrow.clockwise").rotationEffect(.degrees(store.loading ? 360 : 0))
                    .animation(store.loading ? .linear(duration: 0.8).repeatForever(autoreverses: false) : .default, value: store.loading)
            }
            .buttonStyle(IconButton()).keyboardShortcut("r").help(L("刷新 (⌘R)", "Refresh (⌘R)"))
            SettingsMenu()
        }
    }

    private func status(_ c: Stats.Config) -> String {
        switch (c.inject, c.autosave) {
        case (true, true): return L("运行正常", "Running")
        case (false, _): return L("记忆注入已暂停", "Injection paused")
        default: return L("自动保存已关闭", "Auto-save off")
        }
    }
}

struct IconButton: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: .medium))
            .frame(width: 26, height: 26)
            .background(Circle().fill(Color.primary.opacity(configuration.isPressed ? 0.14 : 0.06)))
            .contentShape(Circle())
    }
}

struct SettingsMenu: View {
    @EnvironmentObject var store: Store

    var body: some View {
        Menu {
            Toggle(L("登录时启动", "Launch at login"), isOn: Binding(get: { store.launchAtLogin }, set: { store.setLaunchAtLogin($0) }))
            Toggle(L("菜单栏显示活跃会话数", "Show active sessions in menu bar"), isOn: $store.showCount)
            Divider()
            Button(L("打开数据目录", "Open data folder")) { store.openDataDir() }
            Button(L("运行体检", "Run doctor")) { store.doctor() }
            Button(L("项目主页", "Project page")) { NSWorkspace.shared.open(URL(string: "https://github.com/xiaoxin031/aibase")!) }
            Divider()
            Button(L("退出 aibase", "Quit aibase")) { NSApp.terminate(nil) }
        } label: {
            Image(systemName: "ellipsis")
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .frame(width: 26, height: 26)
        .background(Circle().fill(Color.primary.opacity(0.06)))
    }
}

struct TokensCard: View {
    let a: Stats.Activity
    @State private var hover: Int?

    private var clients: [(String, Tokens)] { a.today.sorted { $0.value.total > $1.value.total } }

    var body: some View {
        Card(title: L("今日 Token", "Tokens today"), trailing: trailing) {
            HStack(alignment: .bottom) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(compact(a.todayTotal))
                        .font(.system(size: 30, weight: .bold, design: .rounded))
                        .monospacedDigit()
                        .numeric()
                        .foregroundStyle(LinearGradient(colors: [Color(Palette.indigo), Color(Palette.violet), Color(Palette.magenta)], startPoint: .leading, endPoint: .trailing))
                        .animation(.easeOut(duration: 0.3), value: a.todayTotal)
                    Text(detail).font(.system(size: 11)).foregroundStyle(.secondary)
                }
                Spacer()
                WeekBars(days: a.days, hover: $hover)
            }
            if a.todayTotal > 0 {
                GeometryReader { g in
                    HStack(spacing: 2) {
                        ForEach(clients, id: \.0) { name, t in
                            Capsule().fill(Color(Palette.client(name)))
                                .frame(width: max(4, (g.size.width - CGFloat(clients.count - 1) * 2) * CGFloat(t.total) / CGFloat(a.todayTotal)))
                        }
                    }
                }
                .frame(height: 6)
                HStack(spacing: 12) {
                    ForEach(clients, id: \.0) { name, t in
                        HStack(spacing: 5) {
                            Circle().fill(Color(Palette.client(name))).frame(width: 7, height: 7)
                            Text(Palette.clientName(name)).font(.system(size: 11, weight: .medium))
                            Text(compact(t.total)).font(.system(size: 11)).monospacedDigit().foregroundStyle(.secondary)
                            Text("\(Int((Double(t.total) / Double(a.todayTotal) * 100).rounded()))%").font(.system(size: 10)).monospacedDigit().foregroundStyle(.tertiary)
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
        let used = a.days.filter { $0.total > 0 }
        guard !used.isEmpty else { return L("近 7 天", "Last 7 days") }
        return L("日均 ", "Avg ") + compact(used.map(\.total).reduce(0, +) / Int64(used.count))
    }

    private var detail: String {
        var t = Tokens()
        for (_, v) in a.today {
            t.input += v.input; t.output += v.output; t.cacheRead += v.cacheRead; t.cacheWrite += v.cacheWrite
        }
        let prompt = t.input + t.cacheRead + t.cacheWrite
        let hit = prompt > 0 ? Int((Double(t.cacheRead) / Double(prompt) * 100).rounded()) : 0
        return L("输入 \(compact(t.input)) · 输出 \(compact(t.output)) · 缓存命中 \(hit)%", "In \(compact(t.input)) · Out \(compact(t.output)) · Cache hit \(hit)%")
    }
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
        HStack(alignment: .bottom, spacing: 4) {
            ForEach(Array(days.enumerated()), id: \.offset) { i, d in
                let today = i == days.count - 1
                VStack(spacing: 3) {
                    RoundedRectangle(cornerRadius: 2.5, style: .continuous)
                        .fill(today ? AnyShapeStyle(LinearGradient(colors: [Color(Palette.violet), Color(Palette.indigo)], startPoint: .top, endPoint: .bottom)) : AnyShapeStyle(Color.primary.opacity(hover == i ? 0.38 : 0.16)))
                        .frame(width: 10, height: max(3, 36 * CGFloat(d.total) / CGFloat(peak)))
                        .opacity(hover == nil || hover == i ? 1 : 0.55)
                    Text(Self.iso.date(from: d.date).map { Self.weekday.string(from: $0) } ?? "")
                        .font(.system(size: 8, weight: today ? .bold : .regular))
                        .foregroundStyle(today ? AnyShapeStyle(Color(Palette.violet)) : AnyShapeStyle(.tertiary))
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

struct SessionsCard: View {
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
        Card(title: L("活跃会话", "Active sessions"), trailing: sessions.isEmpty ? L("15 分钟内", "last 15 min") : L("\(sessions.count) 个运行中", "\(sessions.count) running")) {
            if sessions.isEmpty {
                HStack(spacing: 6) {
                    Image(systemName: "moon.zzz").foregroundStyle(.tertiary)
                    Text(L("当前没有正在进行的 AI 会话", "No AI sessions running right now")).foregroundStyle(.secondary)
                }
                .font(.system(size: 12))
            } else {
                let g = groups
                VStack(spacing: 2) {
                    ForEach(g.prefix(5)) { row($0) }
                    if g.count > 5 {
                        Text(L("还有 \(g.count - 5) 个项目", "\(g.count - 5) more projects")).font(.system(size: 11)).foregroundStyle(.tertiary).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
    }

    private func row(_ g: Group) -> some View {
        Button {
            if let d = g.dir, !d.isEmpty { NSWorkspace.shared.open(URL(fileURLWithPath: d)) }
        } label: {
            HStack(spacing: 8) {
                Pulse(color: Color(Palette.client(g.client)), live: (parseDate(g.updated).map { Date().timeIntervalSince($0) < 120 }) ?? false)
                Text(Palette.clientName(g.client)).font(.system(size: 12, weight: .medium))
                Text(name(g.project)).font(.system(size: 12)).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                if g.count > 1 {
                    Text("×\(g.count)").font(.system(size: 10, weight: .semibold)).monospacedDigit()
                        .padding(.horizontal, 5).padding(.vertical, 1)
                        .background(Capsule().fill(Color(Palette.client(g.client)).opacity(0.16)))
                }
                Spacer()
                Text(relative(g.updated)).font(.system(size: 11)).foregroundStyle(.tertiary)
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

struct Pulse: View {
    let color: Color
    let live: Bool
    @State private var on = false

    var body: some View {
        ZStack {
            if live {
                Circle().fill(color.opacity(0.35)).frame(width: 12, height: 12).scaleEffect(on ? 1 : 0.5).opacity(on ? 0 : 1)
                    .animation(.easeOut(duration: 1.4).repeatForever(autoreverses: false), value: on)
            }
            Circle().fill(color).frame(width: 7, height: 7)
        }
        .frame(width: 12, height: 12)
        .onAppear { on = true }
    }
}

struct MemoryCard: View {
    let s: Stats

    var body: some View {
        Card(title: L("记忆", "Memory")) {
            VStack(spacing: 2) {
                ForEach(s.memory, id: \.target) { u in
                    Button { if let p = u.path { NSWorkspace.shared.open(URL(fileURLWithPath: p)) } } label: { row(u) }
                        .buttonStyle(.plain)
                        .help(u.path ?? "")
                }
            }
            HStack(spacing: 0) {
                Metric(value: s.notes, label: L("笔记", "notes"))
                Metric(value: s.logs, label: L("会话日志", "session logs"))
                Metric(value: s.today["saves"] ?? 0, label: L("今日写入", "saved today"))
                Metric(value: s.today["reviews"] ?? 0, label: L("自动复盘", "auto-reviews"))
            }
            .padding(.top, 2)
        }
    }

    private func row(_ u: Stats.Usage) -> some View {
        let ratio = u.limit > 0 ? min(1, CGFloat(u.used) / CGFloat(u.limit)) : 0
        return HStack(spacing: 8) {
            Text(label(u.target)).font(.system(size: 11, weight: .medium)).frame(width: 56, alignment: .leading)
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.primary.opacity(0.08))
                    Capsule().fill(ratio > 0.9 ? AnyShapeStyle(Color.orange) : AnyShapeStyle(LinearGradient(colors: [Color(Palette.indigo), Color(Palette.violet)], startPoint: .leading, endPoint: .trailing)))
                        .frame(width: max(4, g.size.width * ratio))
                }
            }
            .frame(height: 6)
            Text("\(Int((ratio * 100).rounded()))%").font(.system(size: 10, weight: .medium)).monospacedDigit().frame(width: 30, alignment: .trailing)
                .foregroundStyle(ratio > 0.9 ? AnyShapeStyle(Color.orange) : AnyShapeStyle(.secondary))
                .help("\(u.used)/\(u.limit)")
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

struct Metric: View {
    let value: Int
    let label: String

    var body: some View {
        VStack(spacing: 1) {
            Text("\(value)").font(.system(size: 15, weight: .semibold, design: .rounded)).monospacedDigit().numeric()
                .animation(.easeOut(duration: 0.3), value: value)
            Text(label).font(.system(size: 10)).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
    }
}

struct AgentChip: View {
    let agent: String

    var body: some View {
        Text(Palette.clientName(agent))
            .font(.system(size: 10, weight: .semibold))
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(Capsule().fill(Color(Palette.client(agent)).opacity(0.16)))
    }
}

struct RecentCard: View {
    let recent: [Stats.Recent]
    @State private var open: Int?

    var body: some View {
        Card(title: L("最近会话摘要", "Recent session notes")) {
            VStack(spacing: 4) {
                ForEach(Array(recent.enumerated()), id: \.offset) { i, r in
                    Button { open = open == i ? nil : i } label: {
                        VStack(alignment: .leading, spacing: 3) {
                            HStack(spacing: 6) {
                                AgentChip(agent: (r.agent ?? "").replacingOccurrences(of: "legacy-", with: ""))
                                if let p = r.project, !p.isEmpty { Text(p.split(separator: "-").dropLast().joined(separator: "-")).font(.system(size: 11)).foregroundStyle(.secondary).lineLimit(1) }
                                Spacer()
                                Text(relative(r.ts)).font(.system(size: 10)).foregroundStyle(.tertiary)
                            }
                            Text(summary(r.text, full: open == i)).font(.system(size: 12)).lineLimit(open == i ? 12 : 2)
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
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
            TextField(L("搜索记忆、笔记、历史会话…", "Search memory, notes, sessions…"), text: $query)
                .textFieldStyle(.plain)
                .onExitCommand { query = "" }
            if store.searching { ProgressView().controlSize(.mini) }
            if !query.isEmpty {
                Button { query = "" } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary) }.buttonStyle(.plain)
            }
        }
        .font(.system(size: 12))
        .padding(.horizontal, 10).padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Color.primary.opacity(0.06)))
    }
}

struct SearchResults: View {
    @EnvironmentObject var store: Store
    @State private var open: UUID?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 6) {
                if store.results.isEmpty && !store.searching {
                    VStack(spacing: 6) {
                        Image(systemName: "text.magnifyingglass").font(.system(size: 20)).foregroundStyle(.tertiary)
                        Text(L("没有找到相关记忆", "No matches")).font(.system(size: 12)).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity).padding(.top, 40)
                }
                ForEach(store.results) { h in
                    Button { open = open == h.id ? nil : h.id } label: { hit(h) }.buttonStyle(.plain)
                }
            }
        }
        .frame(height: 360)
        .animation(.easeOut(duration: 0.18), value: open)
    }

    private func hit(_ h: Hit) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: h.note ? "doc.text" : "clock.arrow.circlepath")
                    .font(.system(size: 11)).foregroundStyle(Color(Palette.violet)).frame(width: 14)
                Text(h.title).font(.system(size: 12, weight: .semibold)).lineLimit(1)
                Spacer(minLength: 4)
                if !h.agent.isEmpty { AgentChip(agent: h.agent) }
                if !h.note { Text(h.detail).font(.system(size: 10)).monospacedDigit().foregroundStyle(.tertiary) }
            }
            if h.note { Text(h.detail).font(.system(size: 10, design: .monospaced)).foregroundStyle(.tertiary).lineLimit(1) }
            Text(h.body).font(.system(size: 11)).foregroundStyle(.secondary).lineLimit(open == h.id ? nil : 3)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Color.primary.opacity(open == h.id ? 0.08 : 0.045)))
        .contentShape(Rectangle())
    }
}

struct Footer: View {
    let s: Stats
    @EnvironmentObject var store: Store

    var body: some View {
        let wired = s.clients.filter(\.configured)
        HStack(spacing: 14) {
            Toggle(L("注入", "Inject"), isOn: Binding(get: { s.config.inject }, set: { store.set("inject", $0) }))
            Toggle(L("自动保存", "Auto-save"), isOn: Binding(get: { s.config.autosave }, set: { store.set("autosave", $0) }))
            Spacer()
            HStack(spacing: 5) {
                HStack(spacing: -3) {
                    ForEach(wired.prefix(5), id: \.name) { c in
                        Circle().fill(Color(Palette.client(c.name))).frame(width: 9, height: 9)
                            .overlay(Circle().strokeBorder(Color(nsColor: .windowBackgroundColor), lineWidth: 1.5))
                    }
                }
                Text(L("\(wired.count) 个客户端", "\(wired.count) clients")).foregroundStyle(.secondary)
            }
            .help(wired.map { Palette.clientName($0.name) }.joined(separator: ", "))
        }
        .toggleStyle(Switch())
        .font(.system(size: 11))
        .padding(.horizontal, 2)
    }
}

struct Switch: ToggleStyle {
    func makeBody(configuration: Configuration) -> some View {
        Button { configuration.isOn.toggle() } label: {
            HStack(spacing: 6) {
                configuration.label
                ZStack(alignment: configuration.isOn ? .trailing : .leading) {
                    Capsule()
                        .fill(configuration.isOn ? AnyShapeStyle(LinearGradient(colors: [Color(Palette.indigo), Color(Palette.violet)], startPoint: .leading, endPoint: .trailing)) : AnyShapeStyle(Color.primary.opacity(0.15)))
                        .frame(width: 28, height: 16)
                    Circle().fill(.white).shadow(color: .black.opacity(0.2), radius: 1, y: 0.5).frame(width: 12, height: 12).padding(2)
                }
                .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isOn)
            }
        }
        .buttonStyle(.plain)
    }
}

struct Problem: View {
    let message: String
    @EnvironmentObject var store: Store

    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 22)).foregroundStyle(.orange)
            Text(message).font(.system(size: 12)).multilineTextAlignment(.center).textSelection(.enabled)
            Button(L("重试", "Retry")) { store.refresh() }
        }
        .padding(.vertical, 20)
        .frame(maxWidth: .infinity)
    }
}
