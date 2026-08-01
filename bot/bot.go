package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/oauth2"
)

type Vec3 struct {
	X, Y, Z float64
}

type PlayerInfo struct {
	UUID      uuid.UUID
	XUID      string
	Username  string
	RuntimeID uint64
	Pos       Vec3
	HasPos    bool
	DeviceID  string
	DeviceOS  int32
	Perm      byte
	CmdPerm   byte
	Host      bool
	SeenBy    string
}

func DeviceOSName(v int32) string {
	switch protocol.DeviceOS(v) {
	case protocol.DeviceAndroid:
		return "Android"
	case protocol.DeviceIOS:
		return "iOS"
	case protocol.DeviceOSX:
		return "macOS"
	case protocol.DeviceFireOS:
		return "FireOS"
	case protocol.DeviceHololens:
		return "Hololens"
	case protocol.DeviceWin10:
		return "Windows"
	case protocol.DeviceWin32:
		return "Win32"
	case protocol.DeviceDedicated:
		return "Dedicated"
	case protocol.DeviceOrbis:
		return "PlayStation"
	case protocol.DeviceNX:
		return "Nintendo Switch"
	case protocol.DeviceXBOX:
		return "Xbox"
	case protocol.DeviceLinux:
		return "Linux"
	case 0:
		return "unknown"
	}
	return fmt.Sprintf("unknown (%d)", v)
}

func PermName(v byte) string {
	switch v {
	case 0:
		return "visitor"
	case 1:
		return "member"
	case 2:
		return "operator"
	case 3:
		return "custom"
	}
	return fmt.Sprintf("%d", v)
}

type Bot struct {
	Name string
	Host string

	conn      *minecraft.Conn
	mu        sync.RWMutex
	pos       Vec3
	players   map[string]*PlayerInfo
	byRuntime map[uint64]*PlayerInfo
	connected bool

	OnLog   func(name, msg string)
	LobbyFn func() string
}

func New(name, host string) *Bot {
	return &Bot{
		Name:      name,
		Host:      host,
		players:   make(map[string]*PlayerInfo),
		byRuntime: make(map[uint64]*PlayerInfo),
	}
}

func (b *Bot) Connect(ctx context.Context, tokenSrc oauth2.TokenSource) error {
	b.mu.RLock()
	host := b.Host
	b.mu.RUnlock()

	conn, err := minecraft.Dialer{
		TokenSource:          tokenSrc,
		DownloadResourcePack: func(_ uuid.UUID, _ string, _, _ int) bool { return false },
		EnableClientCache:    true,
		ClientData: login.ClientData{
			DeviceOS:         protocol.DeviceWin10,
			DefaultInputMode: packet.InputModeMouse,
			CurrentInputMode: packet.InputModeMouse,
			LanguageCode:     "en_US",
		},
	}.DialContext(ctx, "raknet", host)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	if err := conn.DoSpawn(); err != nil {
		conn.Close()
		return fmt.Errorf("spawn: %w", err)
	}

	gd  := conn.GameData()
	actualAddr :=  conn.RemoteAddr().String()
	b.mu.Lock()
	b.conn = conn
	b.connected = true
	b.Host = actualAddr
	b.pos = Vec3{
		float64(gd.PlayerPosition[0]),
		float64(gd.PlayerPosition[1]) - 1.62,
		float64(gd.PlayerPosition[2]),
	}
	b.mu.Unlock()

	b.log("Spawned on " + actualAddr)
	go b.readLoop()
	return nil
}

func (b *Bot) readLoop() {
	b.mu.RLock()
	conn := b.conn
	b.mu.RUnlock()
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			b.mu.Lock()
			if b.conn == conn {
				b.connected = false
			}
			b.mu.Unlock()
			b.log("Disconnected: " + err.Error())
			return
		}
		b.handlePacket(pk)
	}
}

func (b *Bot) handlePacket(pk packet.Packet) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch p := pk.(type) {
	case *packet.MovePlayer:
		if p.EntityRuntimeID == b.conn.GameData().EntityRuntimeID {
			b.pos = Vec3{float64(p.Position[0]), float64(p.Position[1]) - 1.62, float64(p.Position[2])}
		} else if info, ok := b.byRuntime[p.EntityRuntimeID]; ok {
			info.Pos = Vec3{float64(p.Position[0]), float64(p.Position[1]) - 1.62, float64(p.Position[2])}
			info.HasPos = true
		} // no idea why

	case *packet.MoveActorAbsolute:
		if info, ok := b.byRuntime[p.EntityRuntimeID]; ok {
			info.Pos = Vec3{float64(p.Position[0]), float64(p.Position[1]), float64(p.Position[2])}
			info.HasPos = true
		}

	case *packet.AddPlayer:
		info := b.players[p.Username]
		if info == nil {
			info = &PlayerInfo{}
			b.players[p.Username] = info
		}
		info.Username = p.Username
		info.UUID = p.UUID
		info.RuntimeID = p.EntityRuntimeID
		info.DeviceID = p.DeviceID
		info.DeviceOS = p.BuildPlatform
		info.Perm = p.AbilityData.PlayerPermissions
		info.CmdPerm = p.AbilityData.CommandPermissions
		info.Pos = Vec3{float64(p.Position[0]), float64(p.Position[1]), float64(p.Position[2])}
		info.HasPos = true
		b.byRuntime[p.EntityRuntimeID] = info

	case *packet.Transfer:
		newAddr := fmt.Sprintf("%s:%d", p.Address, p.Port)
		target := newAddr
		if b.LobbyFn != nil {
			if l := b.LobbyFn(); l != "" && l != b.Host {
				target = l
			}
		}
		b.Host = target
		b.log("Transfer to " + target)
		name := b.Name
		go func() {
			b.Disconnect()
			tokenSrc, err := LoadOrAuth(name, LogWriter)
			if err != nil {
				b.log("Transfer auth: " + err.Error())
				return
			}
			if err := b.Connect(context.Background(), tokenSrc); err != nil {
				b.log("Transfer reconnect: " + err.Error())
			}
		}()



	case *packet.PlayerList:
		for _, entry := range p.Entries {
			if p.ActionType == packet.PlayerListActionAdd {
				info := b.players[entry.Username]
				if info == nil {
					info = &PlayerInfo{}
					b.players[entry.Username] = info
				}
				info.Username = entry.Username
				info.UUID = entry.UUID
				info.XUID = entry.XUID
				info.Host = entry.Host
				if entry.BuildPlatform != 0 {
					info.DeviceOS = entry.BuildPlatform
				}
			} else {
				for name, info := range b.players {
					if info.UUID == entry.UUID {
						delete(b.byRuntime, info.RuntimeID)
						delete(b.players, name)
						break
					}
				}
			}
		}
	}
}

func (b *Bot) Chat(msg string) {
	b.mu.RLock()
	conn := b.conn
	b.mu.RUnlock()
	if conn == nil {
		b.log("Not connected")
		return
	}
	_ = conn.WritePacket(&packet.Text{
		TextType:   packet.TextTypeChat,
		SourceName: b.Name,
		Message:    msg,
	})
	b.log("Chat: " + msg)
}

func (b *Bot) Disconnect() {
	b.mu.Lock()
	conn := b.conn
	b.conn = nil
	b.connected = false
	b.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func (b *Bot) Position() Vec3 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.pos
}




func (b *Bot) Players() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	names := make([]string, 0, len(b.players))
	for name := range b.players {
		names = append(names, name)
	}
	return names
}

func (b *Bot) Lookup(name string) (PlayerInfo, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if info, ok := b.players[name]; ok {
		c := *info
		c.SeenBy = b.Name
		return c, true
	}
	lower := strings.ToLower(name)
	for k, info := range b.players {
		if strings.ToLower(k) == lower {
			c := *info
			c.SeenBy = b.Name
			return c, true
		}
	}
	return PlayerInfo{}, false
}

func (b *Bot) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

func (b *Bot) SetHost(h string) {
	b.mu.Lock()
	b.Host = h
	b.mu.Unlock()
}

func (b *Bot) CurrentServer() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.Host
}

func (b *Bot) log(msg string) {
	if b.OnLog != nil {
		b.OnLog(b.Name, msg)
	}
}
