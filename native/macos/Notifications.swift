import AppKit
import Foundation
import UserNotifications
import CryptoKit
import Darwin

// one source for both identities: the release app and the dev build differ
// only in Info.plist, and the socket is named after whichever this is.
let bundleID = Bundle.main.bundleIdentifier ?? "in.codelif.whatevr"
let appName = Bundle.main.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String ?? "Whatevr"
let ipcQueue = DispatchQueue(label: bundleID + ".ipc")
let center = UNUserNotificationCenter.current()

struct Notice: Codable {
    let id: String
    let chat: String
    let title: String
    let body: String
    let sender: String
    let avatar: String
    let sound: Bool
}
struct Message: Codable {
    var version = 2
    var type: String
    var namespace: String?
    var id: String?
    var chat: String?
    var status: String?
    var notice: Notice?
    // the daemon's socket, in hello, so a click can wake it later
    var socket: String?
    // a link the app was handed
    var url: String?
}
func statusName(_ status: UNAuthorizationStatus) -> String {
    switch status {
    case .notDetermined: return "not-determined: run whatevrd notifications setup"
    case .denied: return "denied: enable \(appName) in System Settings > Notifications"
    case .authorized: return "authorized"
    case .provisional: return "provisional"
    @unknown default: return "unknown"
    }
}
func userTemp() throws -> String {
    let size = confstr(_CS_DARWIN_USER_TEMP_DIR, nil, 0)
    guard size > 0 else { throw NSError(domain: bundleID, code: 1) }
    var bytes = [CChar](repeating: 0, count: size)
    guard confstr(_CS_DARWIN_USER_TEMP_DIR, &bytes, size) > 0 else { throw NSError(domain: bundleID, code: 1) }
    return String(cString: bytes)
}
// the socket the login service holds, platform.NativeSocketPath. links go to
// whichever daemon sits on it
func defaultSocket() -> String? {
    guard let temp = try? userTemp() else { return nil }
    return (temp as NSString).appendingPathComponent("in.codelif.whatevr.sock")
}
// a daemon's namespace is the first 16 bytes of sha256 of its socket path
func namespace(of socket: String) -> String {
    SHA256.hash(data: Data(socket.utf8)).prefix(16).map { String(format: "%02x", $0) }.joined()
}
func unixAddress(_ path: String) throws -> sockaddr_un {
    var address = sockaddr_un()
    address.sun_family = sa_family_t(AF_UNIX)
    let bytes = Array(path.utf8) + [0]
    guard bytes.count <= MemoryLayout.size(ofValue: address.sun_path) else { throw NSError(domain: bundleID, code: 2) }
    address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
    withUnsafeMutableBytes(of: &address.sun_path) { raw in raw.copyBytes(from: bytes) }
    return address
}
func privateDirectory(_ path: String) throws {
    try FileManager.default.createDirectory(atPath: path, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    var info = stat()
    guard lstat(path, &info) == 0, info.st_mode & S_IFMT == S_IFDIR,
          info.st_mode & 0o077 == 0, info.st_uid == geteuid() else {
        throw NSError(domain: bundleID, code: 3, userInfo: [NSLocalizedDescriptionKey: "Notification socket directory is not private"])
    }
}

final class Peer {
    let fd: Int32
    let server: Server
    var namespace: String?
    var data = Data()
    var source: DispatchSourceRead?
    var closed = false
    init(fd: Int32, server: Server) { self.fd = fd; self.server = server }
    func start() {
        var noPipe: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &noPipe, socklen_t(MemoryLayout<Int32>.size))
        var timeout = timeval(tv_sec: 5, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
        let source = DispatchSource.makeReadSource(fileDescriptor: fd, queue: ipcQueue)
        self.source = source
        source.setEventHandler { [weak self] in self?.read() }
        source.setCancelHandler { [fd] in Darwin.close(fd) }
        source.resume()
    }
    func stop() {
        if closed { return }
        closed = true
        source?.cancel()
        server.peers.removeValue(forKey: fd)
    }
    func read() {
        var bytes = [UInt8](repeating: 0, count: 8192)
        let count = Darwin.read(fd, &bytes, bytes.count)
        if count <= 0 { stop(); return }
        data.append(contentsOf: bytes.prefix(count))
        if data.count > 1 << 20 { stop(); return }
        while let newline = data.firstIndex(of: 10) {
            let line = data.prefix(upTo: newline)
            data.removeSubrange(...newline)
            guard let message = try? JSONDecoder().decode(Message.self, from: line), message.version == 2 else { stop(); return }
            handle(message)
        }
    }
    func send(_ message: Message) {
        guard !closed, var bytes = try? JSONEncoder().encode(message) else { return }
        bytes.append(10)
        bytes.withUnsafeBytes { raw in
            var offset = 0
            while offset < raw.count {
                let n = Darwin.write(fd, raw.baseAddress!.advanced(by: offset), raw.count - offset)
                if n < 0 && errno == EINTR { continue }
                if n <= 0 { stop(); break }
                offset += n
            }
        }
    }
    func settings() {
        center.getNotificationSettings { [weak self] settings in
            let status = statusName(settings.authorizationStatus)
            ipcQueue.async { self?.send(Message(type: "status", status: status)) }
        }
    }
    func handle(_ message: Message) {
        switch message.type {
        case "hello":
            guard let ns = message.namespace, ns.count == 32, ns.allSatisfy({ $0.isHexDigit }) else { stop(); return }
            namespace = ns
            if let socket = message.socket, socket.hasPrefix("/"), socket.utf8.count < 104 {
                server.sockets[ns] = socket
            }
            for item in server.pending.removeValue(forKey: ns) ?? [] where Date().timeIntervalSince(item.0) < 30 {
                send(item.1)
            }
            settings()
        case "status": settings()
        case "quit-if-idle":
            if server.peers.values.contains(where: { $0.namespace != nil }) {
                send(Message(type: "status", status: "busy"))
            } else {
                send(Message(type: "status", status: "stopping"))
                DispatchQueue.main.async { NSApp.terminate(nil) }
            }
        case "authorize":
            center.requestAuthorization(options: [.alert, .sound]) { [weak self] _, _ in self?.settings() }
        case "show":
            guard let ns = namespace, ns == message.namespace, let notice = message.notice else { stop(); return }
            let identifier = ns + ":" + notice.id
            let generation = UUID()
            server.generations[identifier] = generation
            center.getNotificationSettings { [weak self] settings in
                guard settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional else { return }
                let content = UNMutableNotificationContent()
                content.title = String(notice.title.prefix(200))
                content.subtitle = String(notice.sender.prefix(200))
                content.body = String(notice.body.prefix(200))
                content.userInfo = ["namespace": ns, "chat": notice.chat]
                content.threadIdentifier = ns + ":" + notice.chat
                if notice.sound && settings.soundSetting == .enabled { content.sound = .default }
                ipcQueue.async { [weak self] in
                    guard let self, self.server.generations[identifier] == generation else { return }
                    // Replacements keep one visible notification per daemon notification id.
                    center.removeDeliveredNotifications(withIdentifiers: [identifier])
                    center.add(UNNotificationRequest(identifier: identifier, content: content, trigger: nil)) { error in
                        if let error { NSLog("Whatevr notification delivery: %@", error.localizedDescription) }
                    }
                }
            }
        case "close":
            guard let ns = namespace, ns == message.namespace, let id = message.id else { stop(); return }
            let identifier = ns + ":" + id
            server.generations.removeValue(forKey: identifier)
            center.removePendingNotificationRequests(withIdentifiers: [identifier])
            center.removeDeliveredNotifications(withIdentifiers: [identifier])
        default: stop()
        }
    }
}

final class Server {
    var peers: [Int32: Peer] = [:]
    var generations: [String: UUID] = [:]
    // what came in for a daemon that wasn't connected: clicks, links, opens
    var pending: [String: [(Date, Message)]] = [:]
    var sockets: [String: String] = [:]
    var listener: Int32 = -1
    var lock: Int32 = -1
    var source: DispatchSourceRead?
    func start() throws {
        let dir = try (userTemp() as NSString).appendingPathComponent("whatevr")
        try privateDirectory(dir)
        lock = Darwin.open((dir as NSString).appendingPathComponent(bundleID + ".lock"), O_CREAT | O_RDWR | O_NOFOLLOW, 0o600)
        guard lock >= 0 else { throw NSError(domain: bundleID, code: 4) }
        guard flock(lock, LOCK_EX | LOCK_NB) == 0 else { throw NSError(domain: bundleID, code: 5, userInfo: [NSLocalizedDescriptionKey: "Notification helper is already running"]) }
        let path = (dir as NSString).appendingPathComponent(bundleID + ".sock")
        listener = socket(AF_UNIX, SOCK_STREAM, 0)
        guard listener >= 0 else { throw NSError(domain: bundleID, code: 6) }
        _ = fcntl(listener, F_SETFL, O_NONBLOCK)
        unlink(path)
        var address = try unixAddress(path)
        let bound = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(listener, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard bound == 0, chmod(path, 0o600) == 0, listen(listener, 16) == 0 else { throw NSError(domain: bundleID, code: 7) }
        let source = DispatchSource.makeReadSource(fileDescriptor: listener, queue: ipcQueue)
        self.source = source
        source.setEventHandler { [weak self] in self?.acceptPeers() }
        source.resume()
    }
    func acceptPeers() {
        while true {
            let fd = accept(listener, nil, nil)
            if fd < 0 { return }
            var uid: uid_t = 0
            var gid: gid_t = 0
            guard getpeereid(fd, &uid, &gid) == 0, uid == geteuid(), peers.count < 64 else { Darwin.close(fd); continue }
            _ = fcntl(fd, F_SETFL, 0)
            let peer = Peer(fd: fd, server: self)
            peers[fd] = peer
            peer.start()
        }
    }
    func clicked(namespace: String, chat: String) {
        ipcQueue.async { self.deliver(Message(type: "click", namespace: namespace, chat: chat)) }
    }
    // links and plain opens go to the daemon on the default socket
    func link(_ url: String) {
        ipcQueue.async {
            guard let socket = defaultSocket() else { return }
            self.deliver(Message(type: "link", namespace: namespace(of: socket), url: url))
        }
    }
    func activate() {
        ipcQueue.async {
            guard let socket = defaultSocket() else { return }
            self.deliver(Message(type: "activate", namespace: namespace(of: socket)))
        }
    }
    func deliver(_ message: Message) {
        guard let ns = message.namespace else { return }
        let recipients = peers.values.filter { $0.namespace == ns }
        for peer in recipients { peer.send(message) }
        if !recipients.isEmpty { return }
        // held for the daemon's hello. a click can relaunch the helper
        // before a daemon reconnects, and a stopped one gets woken
        if pending.count >= 64 { pending.removeAll() }
        pending[ns] = Array(((pending[ns] ?? []) + [(Date(), message)]).suffix(16))
        wake(ns)
    }
    // wake connects to the daemon's socket, which starts it when the login
    // service holds it. nothing listening means the service is off.
    func wake(_ ns: String) {
        var path = sockets[ns]
        if path == nil, let socket = defaultSocket(), namespace(of: socket) == ns { path = socket }
        guard let path, var address = try? unixAddress(path) else { return }
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { return }
        defer { Darwin.close(fd) }
        let connected = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        if connected != 0 && (errno == ENOENT || errno == ECONNREFUSED) {
            pending.removeValue(forKey: ns)
            notifyLocally(title: "\(appName) isn't running", body: "Start it with: whatevrd service enable")
        }
    }
    func notifyLocally(title: String, body: String) {
        center.getNotificationSettings { settings in
            guard settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional else { return }
            let content = UNMutableNotificationContent()
            content.title = title
            content.body = body
            center.add(UNNotificationRequest(identifier: "local:" + UUID().uuidString, content: content, trigger: nil))
        }
    }
}
final class Delegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    let server = Server()
    // a launch for a link or a click isn't a plain open
    var handled = false
    func applicationDidFinishLaunching(_ notification: Notification) {
        center.delegate = self
        do { try server.start() }
        catch { NSLog("Whatevr notifications: %@", error.localizedDescription); NSApp.terminate(nil) }
        // the daemon starts the app with --background. anyone else opening
        // it with nothing in hand wants a frontend; a link or click arrives
        // just after launch, so give it a moment
        if !ProcessInfo.processInfo.arguments.contains("--background") {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { [self] in
                if !handled { server.activate() }
            }
        }
    }
    func application(_ application: NSApplication, open urls: [URL]) {
        handled = true
        for url in urls.prefix(16) { server.link(url.absoluteString) }
    }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        server.activate()
        return false
    }
    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification, withCompletionHandler completion: @escaping (UNNotificationPresentationOptions) -> Void) {
        completion([.banner, .list, .sound])
    }
    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completion: @escaping () -> Void) {
        DispatchQueue.main.async { self.handled = true }
        let info = response.notification.request.content.userInfo
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier,
           let namespace = info["namespace"] as? String, let chat = info["chat"] as? String {
            server.clicked(namespace: namespace, chat: chat)
        }
        completion()
    }
}
let app = NSApplication.shared
let delegate = Delegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
