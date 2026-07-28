package main

import (
	"nbot/bot"
	"nbot/cli"
	"nbot/proxy"
)

func main() {
	manager :=   bot.NewManager()

	handler := &cli.Handler{
		Manager: manager,
	}
	handler.LogFn = func(name, msg string) {
		cli.Log(name + ": " + msg)
	}


	handler.StartProxy = func(acct string) error { return proxy.Start(handler, acct) }
	handler.WarmProxy = proxy.Warm

	bot.LogWriter = cli.AuthWriter



	cli.Run(handler)
}
