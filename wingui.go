// +build windows

package main

import (
	"fmt"

	"github.com/hdpool/hdminer/liboo"

	"github.com/getlantern/systray"
)

func onReady() {
	oo.LogD("WinGui onReady")

	systray.SetIcon(trayicon)
	systray.SetTitle("Hdpool App")
	systray.SetTooltip("Hdpool")

	mQuitOrig := systray.AddMenuItem("Quit", "Quit the whole app")

	systray.AddSeparator()
	mToggle := systray.AddMenuItem("Open App", "Open App")
	for {
		select {

		case <-mToggle.ClickedCh:
			openUrlWithDefaultBrowser(fmt.Sprintf("http://127.0.0.1:%d", conf.Port))
		case <-mQuitOrig.ClickedCh:
			oo.LogD("Requesting quit")
			ExitAll()
		}
	}
}

type WinGui struct {
}

func InitGui() *WinGui {
	it := WinGui{}

	go systray.Run(onReady, nil)

	return &it
}

func QuitGui() {
	systray.Quit()
}
