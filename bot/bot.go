package bot

import (
	"context"
	"fmt"
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
	UUID uuid.UUID
}

type Bot struct {
	Name string
	Host string

	conn      *minecraft.Conn
	mu        sync.RWMutex
	pos       Vec3
	players   map[string]*PlayerInfo
	connected bool

	OnLog   func(name, msg string)
	LobbyFn func() string
}

func New(name, host string) *Bot {
	return &Bot{
		Name:    name,
		Host:    host,
		players: make(map[string]*PlayerInfo),
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
		} // no idea why

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
				if _, ok := b.players[entry.Username]; !ok {
					b.players[entry.Username] = &PlayerInfo{UUID: entry.UUID}
				}
			} else {
				for name, info := range b.players {
					if info.UUID == entry.UUID {
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
