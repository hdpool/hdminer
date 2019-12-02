// +build linux darwin !windows

package main

type WinGui struct {
}

func InitGui() *WinGui {
	it := WinGui{}

	return &it
}

func QuitGui() {
}
