package cli

// For anyone who is looking here, claude mostly made this, so yeah, call me a vibe coder if you want but not bad for claude ngl.
import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"nbot/bot"
)

type Handler struct {
	Manager    *bot.Manager
	LogFn      func(name, msg string)
	StartProxy func(account string) error
	WarmProxy  func(account string)

	serverMu sync.RWMutex
	server   string
	account  string

	spamMu     sync.Mutex
	spamCancel context.CancelFunc
}

func (h *Handler) SetServer(s string) {
	h.serverMu.Lock()
	h.server = s
	h.serverMu.Unlock()
}

func (h *Handler) GetServer() string {
	h.serverMu.RLock()
	defer h.serverMu.RUnlock()
	return h.server
}

func (h *Handler) SetAccount(a string) {
	h.serverMu.Lock()
	h.account = a
	h.serverMu.Unlock()
}

func (h *Handler) GetAccount() string {
	h.serverMu.RLock()
	defer h.serverMu.RUnlock()
	return h.account
}

func (h *Handler) target() string {
	if l := h.Manager.Lobby(); l != "" {
		return l
	}
	return h.GetServer()
}

const prompt = "(Enter yo commands)>"

var termMu sync.Mutex

func Log(line string) {
	termMu.Lock()
	fmt.Print("\n" + line + "\n")
	termMu.Unlock()
}

func printLines(lines []string) {
	termMu.Lock()
	for _, l := range lines {
		fmt.Println(l)
	}
	termMu.Unlock()
}

type authWriter struct{}

func (authWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\r\n"), "\n") {
		if s := strings.TrimRight(line, "\r"); s != "" {
			Log(s)
		}
	}
	return len(p), nil
}

var AuthWriter = authWriter{}

func (h *Handler) Execute(input string) []string {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "/.")
	if input == "" {
		return nil
	}

	parts := strings.SplitN(input, " ", 2)
	cmd := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "join":
		return h.cmdJoin(args)
	case "pos":
		return h.cmdPos()
	case "debug":
		return h.cmdDebug()
	case "chat":
		return h.cmdChat(args)
	case "unload":
		return h.cmdUnload(args)
	case "load":
		return h.cmdLoad(args)
	case "loadall":
		return h.cmdLoadAll()
	case "spam":
		return h.cmdSpam(args)
	case "disconnect":
		return h.cmdDisconnect(args)
	case "disconnectall":
		return h.cmdDisconnectAll()
	case "listplayers":
		return h.cmdListPlayers()
	case "ip":
		return h.cmdIP()
	case "goto":
		return h.cmdGoto(args)
	case "pair":
		return h.cmdPair(args)
	case "help":
		return helpLines()
	case "clear":
		cmdClear()
		return nil
	default:
		return []string{"Unknown command: " + cmd + " — type help"}
	}
}

func (h *Handler) cmdJoin(args string) []string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return []string{"Usage: join play.lbsg.net [account]"}
	}

	addr := fields[0]
	if !strings.Contains(addr, ":") {
		addr += ":19132"
	}

	saved := h.Manager.ListAvailable()
	account := ""
	if len(fields) > 1 {
		account = fields[1]
	} else if len(saved) > 0 {
		account = saved[0]
	}

	if account == "" {
		return []string{
			"no saved accounts, pair one first: pair Bot1",
		}
	}
	if !bot.HasToken(account) {
		lines := []string{"no token for account: " + account}
		if len(saved) > 0 {
			lines = append(lines, "you have: "+strings.Join(saved, ", "))
			lines = append(lines, "use: join "+fields[0]+" "+saved[0])
		}
		lines = append(lines, "or pair it: pair "+account)
		return lines
	}

	h.SetServer(addr)
	h.SetAccount(account)
	h.Manager.SetLobby("")

	if h.StartProxy != nil {
		if err := h.StartProxy(account); err != nil {
			return []string{"proxy failed to start: " + err.Error()}
		}
	}
	if h.WarmProxy != nil {
		go h.WarmProxy(account)
	}

	return []string{
		"target: " + addr,
		"you join as: " + account,
		"add 127.0.0.1 port 19133 in Minecraft and join it",
		"bots lock onto your lobby once you land, then run loadall",
	}
}

func (h *Handler) cmdPos() []string {
	bots := h.Manager.All()
	if len(bots) == 0 {
		return []string{"No bots loaded"}
	}
	var lines []string
	for _, b := range bots {
		p := b.Position()
		lines = append(lines, fmt.Sprintf("%s x=%.1f y=%.1f z=%.1f", b.Name, p.X, p.Y, p.Z))
	}
	return lines
}

func (h *Handler) cmdDebug() []string {
	bots := h.Manager.All()
	online := 0
	for _, b := range bots {
		if b.IsConnected() {
			online++
		}
	}
	lines := []string{fmt.Sprintf("Bots: %d total, %d online", len(bots), online)}
	for _, b := range bots {
		status := "offline"
		if b.IsConnected() {
			status = "online"
		}
		lines = append(lines, fmt.Sprintf("  %s [%s] server: %s", b.Name, status, b.CurrentServer()))
	}
	return lines
}

func (h *Handler) cmdChat(args string) []string {
	msg := strings.Trim(args, `"'`)
	if msg == "" {
		return []string{`Usage: chat <message>`}
	}
	bots := h.Manager.All()
	if len(bots) == 0 {
		return []string{"No bots loaded"}
	}
	for _, b := range bots {
		b.Chat(msg)
	}
	return []string{fmt.Sprintf("Sent chat to %d bot(s)", len(bots))}
}

func (h *Handler) cmdUnload(args string) []string {
	name := strings.TrimSpace(args)
	if name == "" {
		return []string{"Usage: unload Bot5"}
	}
	if err := h.Manager.Disconnect(name); err != nil {
		return []string{"Error: " + err.Error()}
	}
	return []string{"Unloaded: " + name}
}

func (h *Handler) cmdLoad(args string) []string {
	name := strings.TrimSpace(args)
	if name == "" {
		return []string{"Usage: load Bot5"}
	}
	dst := h.target()
	if dst == "" {
		return []string{"No server set, use: join <ip>"}
	}
	if name == h.GetAccount() {
		return []string{"you are playing as " + name + ", it cannot also be a bot"}
	}
	go func() {
		err := h.Manager.Load(name, dst, h.LogFn)
		if err != nil && h.LogFn != nil {
			h.LogFn(name, "Load failed: "+err.Error())
		}
	}()
	return []string{"Loading " + name + " -> " + dst + " (auth may open browser)"}
}

func (h *Handler) cmdLoadAll() []string {
	all := h.Manager.ListAvailable()
	if len(all) == 0 {
		return []string{"No saved bots found in tokens/"}
	}
	dst := h.target()
	if dst == "" {
		return []string{"No server set, use: join <ip>"}
	}

	me := h.GetAccount()
	var available []string
	for _, n := range all {
		if n != me {
			available = append(available, n)
		}
	}
	if len(available) == 0 {
		return []string{
			"only account is " + me + " and you are playing as it",
			"pair another one: pair Bot2",
		}
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Loading %d bot(s) -> %s", len(available), dst))
	for _, name := range available {
		n := name
		go func() {
			err := h.Manager.Load(n, dst, h.LogFn)
			if err != nil && h.LogFn != nil {
				h.LogFn(n, "Load failed: "+err.Error())
			}
		}()
		lines = append(lines, "  Loading: "+n)
	}
	return lines
}

func (h *Handler) cmdSpam(args string) []string {
	if strings.TrimSpace(args) == "stop" {
		h.spamMu.Lock()
		if h.spamCancel != nil {
			h.spamCancel()
			h.spamCancel = nil
			h.spamMu.Unlock()
			return []string{"Spam stopped"}
		}
		h.spamMu.Unlock()
		return []string{"No spam running"}
	}

	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		return []string{`Usage: spam <seconds> <message>  |  spam stop`}
	}

	seconds, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || seconds < 1 || seconds > 10000000000 {
		return []string{"Delay must be 1-10000000000 seconds"}
	}

	msg := strings.TrimSpace(parts[1])
	if msg == "" {
		return []string{`Usage: spam <seconds> <message>  |  spam stop`}
	}

	h.spamMu.Lock()
	if h.spamCancel != nil {
		h.spamCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.spamCancel = cancel
	h.spamMu.Unlock()

	delay := time.Duration(float64(time.Second) * seconds)

	go func() {
		for {
			bots := h.Manager.All()
			for _, b := range bots {
				b.Chat(msg)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}()

	return []string{fmt.Sprintf("Spamming every %.0fs: %s", seconds, msg)}
}

func (h *Handler) cmdDisconnect(args string) []string {
	name := strings.TrimSpace(args)
	if name == "" {
		return []string{"Usage: disconnect Bot5"}
	}
	if err := h.Manager.Disconnect(name); err != nil {
		return []string{"Error: " + err.Error()}
	}
	return []string{"Disconnected: " + name}
}

func (h *Handler) cmdDisconnectAll() []string {
	bots := h.Manager.All()
	if len(bots) == 0 {
		return []string{"No bots to disconnect"}
	}
	h.Manager.DisconnectAll()
	return []string{fmt.Sprintf("Disconnected %d bot(s)", len(bots))}
}

func (h *Handler) cmdIP() []string {
	srv := h.GetServer()
	if srv == "" {
		srv = "(none, use join <ip>)"
	}
	lines := []string{"target: " + srv, "you join: 127.0.0.1:19133"}
	if l := h.Manager.Lobby(); l != "" {
		lines = append(lines, "lobby locked: "+l)
	} else {
		lines = append(lines, "lobby: not locked yet")
	}
	for _, b := range h.Manager.All() {
		if b.IsConnected() {
			lines = append(lines, b.Name+" on "+b.CurrentServer())
		}
	}
	return lines
}

func (h *Handler) cmdGoto(args string) []string {
	addr := strings.TrimSpace(args)
	if addr == "" {
		return []string{"Usage: goto 51.81.211.36:19132"}
	}
	if !strings.Contains(addr, ":") {
		addr += ":19132"
	}
	h.Manager.SetLobby(addr)
	if len(h.Manager.All()) == 0 {
		return []string{"lobby set to " + addr + ", bots will use it on load"}
	}
	h.Manager.ReconnectAll(addr, h.LogFn)
	return []string{"Moving all bots to " + addr}
}

func (h *Handler) cmdPair(args string) []string {
	name := strings.TrimSpace(args)
	if name == "" {
		return []string{"Usage: pair <name>"}
	}
	go func() {
		err := h.Manager.Load(name, h.target(), h.LogFn)
		if err != nil && h.LogFn != nil {
			h.LogFn(name, "Pair failed: "+err.Error())
		}
	}()
	return []string{"Pairing " + name + " (browser will open for Xbox auth)"}
}

func (h *Handler) cmdListPlayers() []string {
	bots := h.Manager.All()
	if len(bots) == 0 {
		return []string{"No bots loaded"}
	}

	seen := make(map[string]struct{})
	for _, b := range bots {
		for _, p := range b.Players() {
			seen[p] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return []string{"No players visible"}
	}
	lines := []string{fmt.Sprintf("Players (%d):", len(seen))}
	for name := range seen {
		lines = append(lines, "  "+name)
	}
	return lines
}

func cmdClear() {
	termMu.Lock()
	c := exec.Command("cmd", "/c", "cls")
	c.Stdout = os.Stdout
	_ = c.Run()
	printHeader()

	termMu.Unlock()
}

func printHeader() {
	fmt.Println("Nbot - Version: 0.1.5 Beta - Made with love from Liuxium Labs!")
	fmt.Println("- Yes bro, I used claude for dis 100 percent (jk claude was used but for the \"ui\" and some of the chat logic)")
	fmt.Println("- By Linus Co. (liuxium co. yep)")
	fmt.Println()
	fmt.Println("Current commands:")
	fmt.Println()
	for _, line := range helpLines() {
		fmt.Println(line)
	}
}

func helpLines() []string {
	return []string{
		"join <ip> [acct]   set server, play as acct, proxy on 127.0.0.1:19133",
		"pos                show all bots position",
		"debug              show bot count and status",
		"chat <msg>         send chat with all bots",
		"load <name>        load a bot (load Bot5)",
		"loadall            load all saved bots",
		"unload <name>      unload a bot",
		"spam <s> <msg>     spam message every S seconds",
		"spam stop          stop spamming",
		"disconnect <name>  disconnect a bot",
		"disconnectall      disconnect all bots",
		"listplayers        list visible players from bots",
		"ip                 show hub and where bots are",
		"goto <ip>          move all bots to an exact server ip",
		"pair <name>        pair a new bot account (opens browser)",
		"help               shows this message",
		"clear              clear da chat",
	}
}

func Run(h *Handler) {
	termMu.Lock()
	printHeader()
	fmt.Println()
	termMu.Unlock()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		termMu.Lock()
		fmt.Print(prompt)
		termMu.Unlock()

		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if responses := h.Execute(input); len(responses) > 0 {
			printLines(responses)
		}
	}

	fmt.Println("\nShutting down...")
	h.Manager.DisconnectAll()
}
