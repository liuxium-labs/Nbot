package proxy

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"nbot/bot"
	"nbot/cli"
)

const (
	ListenAddr = ":19133"
	LocalHost  = "127.0.0.1"
	LocalPort  = 19133
)

var (
	mu      sync.Mutex
	started bool
	pending string
	account string
)

func Start(h *cli.Handler, acct string) error {
	mu.Lock()
	defer mu.Unlock()
	account = acct
	if started {
		return nil
	}
	listener, err := minecraft.ListenConfig{
		AuthenticationDisabled: true,
	}.Listen("raknet", ListenAddr)
	if err != nil {
		return err
	}
	started = true
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handle(conn.(*minecraft.Conn), h)
		}
	}()
	return nil
}

func Warm(acct string) {
	if _, err := bot.LoadOrAuth(acct, bot.LogWriter); err != nil {
		cli.Log("proxy account auth failed: " + err.Error())
	}
}

func currentAccount() string {
	mu.Lock()
	defer mu.Unlock()
	return account
}

func setPending(addr string) {
	mu.Lock()
	pending = addr
	mu.Unlock()
}

func takePending() string {
	mu.Lock()
	defer mu.Unlock()
	addr := pending
	pending = ""
	return addr
}

func handle(client *minecraft.Conn, h *cli.Handler) {
	target := takePending()
	if target == "" {
		target = h.GetServer()
	}
	if target == "" {
		cli.Log("proxy has no target, use: join <ip>")
		client.Close()
		return
	}

	server, err := dial(target)
	if err != nil {
		cli.Log("proxy connect failed: " + err.Error())
		client.Close()
		return
	}

	if err := client.StartGame(server.GameData()); err != nil {
		cli.Log("proxy start game failed: " + err.Error())
		client.Close()
		server.Close()
		return
	}

	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		for {
			pk, err := server.ReadPacket()
			if err != nil {
				return
			}
			if t, ok := pk.(*packet.Transfer); ok {
				addr := fmt.Sprintf("%s:%d", t.Address, t.Port)
				setPending(addr)
				lockLobby(h, addr)
				_ = client.WritePacket(&packet.Transfer{Address: LocalHost, Port: LocalPort})
				return
			}
			if err := client.WritePacket(pk); err != nil {
				return
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		for {
			pk, err := client.ReadPacket()
			if err != nil {
				return
			}
			if err := server.WritePacket(pk); err != nil {
				return
			}
		}
	}()

	<-done
	client.Close()
	server.Close()
}

func lockLobby(h *cli.Handler, addr string) {
	if addr == "" || h.Manager.Lobby() == addr {
		return
	}
	h.Manager.SetLobby(addr)
	cli.Log("lobby locked: " + addr)
	if len(h.Manager.All()) > 0 {
		h.Manager.ReconnectAll(addr, h.LogFn)
	}
}

func dial(addr string) (*minecraft.Conn, error) {
	tokenSrc, err := bot.LoadOrAuth(currentAccount(), bot.LogWriter)
	if err != nil {
		return nil, err
	}
	server, err := minecraft.Dialer{
		TokenSource:          tokenSrc,
		DownloadResourcePack: func(_ uuid.UUID, _ string, _, _ int) bool { return false },
		EnableClientCache:    true,
		ClientData: login.ClientData{
			DeviceOS:         protocol.DeviceWin10,
			DefaultInputMode: packet.InputModeMouse,
			CurrentInputMode: packet.InputModeMouse,
			LanguageCode:     "en_US",
		},
	}.DialContext(context.Background(), "raknet", addr)
	if err != nil {
		return nil, err
	}
	if err := server.DoSpawn(); err != nil {
		server.Close()
		return nil, err
	}
	return server, nil
}
